package container

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	mounttypes "github.com/docker/docker/api/types/mount"
	networktypes "github.com/docker/docker/api/types/network"
	volumetypes "github.com/docker/docker/api/types/volume"
)

// EngineSpec describes the engine (inference server) half of a paired run.
type EngineSpec struct {
	Image       string            `json:"image"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Ports       []int             `json:"ports,omitempty"`
	ModelPath   string            `json:"model_path"`
	HealthCheck HealthCheck       `json:"health_check"`
}

// HealthCheck describes how to verify the engine container is serving.
type HealthCheck struct {
	Path           string `json:"path"`
	Port           int    `json:"port"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// BenchmarkSpec describes the benchmark half of a paired run.
type BenchmarkSpec struct {
	Kind     string            `json:"kind"` // "yaml" or "container"
	Image    string            `json:"image"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Config   json.RawMessage   `json:"config,omitempty"`
	YAMLSpec json.RawMessage   `json:"yaml_spec,omitempty"`
}

// PairedRunSpec is the full request for a paired engine+benchmark run.
type PairedRunSpec struct {
	RunID          string        `json:"run_id"`
	TimeoutSeconds int           `json:"timeout_seconds"`
	Engine         EngineSpec    `json:"engine"`
	Benchmark      BenchmarkSpec `json:"benchmark"`
}

// PairedRunEvent is a single streaming event from PairedRun.
type PairedRunEvent struct {
	Type       string          `json:"type"`
	Phase      string          `json:"phase,omitempty"`
	Source     string          `json:"source,omitempty"`
	Line       string          `json:"line,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
	Results    json.RawMessage `json:"results,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// modelMountPath is where the model directory is bind-mounted in the engine
// container. The engine's Args typically reference this path.
const modelMountPath = "/model"

// resultsVolumeMount is where the benchmark writes its output; bonnie reads
// out.json from this location after the benchmark exits.
const resultsVolumeMount = "/results"

// resultsFile is the well-known results artifact the benchmark must produce.
const resultsFile = "out.json"

// validRunID matches safe Docker resource name characters: alphanumeric, dash,
// and underscore. RunIDs are interpolated into Docker network, volume, and
// container names, so shell metacharacters, slashes, or spaces would cause API
// errors or unexpected behaviour.
var validRunID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateRunID returns an error if id contains characters unsafe for use in
// Docker resource names.
func ValidateRunID(id string) error {
	if !validRunID.MatchString(id) {
		return fmt.Errorf("run_id %q is invalid: must match [a-zA-Z0-9_-]+", id)
	}
	return nil
}

// defaultEngineHealthTimeout is used when HealthCheck.TimeoutSeconds <= 0.
const defaultEngineHealthTimeout = 300

// PairedRun orchestrates the engine + benchmark container pair. events is
// closed when the run terminates; callers can stream events to clients over
// SSE. Cleanup (stopping and removing both containers, the network, and the
// results volume) always runs, even on context cancel or error.
//
//nolint:gocritic // hugeParam: spec is intentionally passed by value.
func (m *Manager) PairedRun(ctx context.Context, spec PairedRunSpec, events chan<- PairedRunEvent) error {
	defer close(events)

	if spec.RunID == "" {
		return fmt.Errorf("paired run: run_id is required")
	}
	if err := ValidateRunID(spec.RunID); err != nil {
		return fmt.Errorf("paired run: %w", err)
	}
	if spec.Engine.Image == "" {
		return fmt.Errorf("paired run: engine.image is required")
	}
	if spec.Benchmark.Image == "" {
		return fmt.Errorf("paired run: benchmark.image is required")
	}
	if spec.Benchmark.Kind != "" && spec.Benchmark.Kind != "yaml" && spec.Benchmark.Kind != "container" {
		return fmt.Errorf("paired run: benchmark.kind must be 'yaml' or 'container'")
	}

	start := time.Now()

	networkName := "bonnie-run-" + spec.RunID
	volumeName := "bonnie-results-" + spec.RunID
	engineName := "bonnie-engine-" + spec.RunID
	benchName := "bonnie-bench-" + spec.RunID

	state := &runState{events: events, start: start}

	defer func() {
		m.cleanupRun(context.Background(), networkName, volumeName, state)
	}()

	// 1. Create the network.
	state.emit(PairedRunEvent{Type: "status", Phase: "creating-network", Source: "orchestrator"})
	if _, err := m.client.NetworkCreate(ctx, networkName, networktypes.CreateOptions{Driver: "bridge"}); err != nil {
		state.emitError("creating-network", err)
		return fmt.Errorf("paired run: create network: %w", err)
	}

	// 2. Create the results volume.
	state.emit(PairedRunEvent{Type: "status", Phase: "creating-volume", Source: "orchestrator"})
	if _, err := m.client.VolumeCreate(ctx, volumetypes.CreateOptions{Name: volumeName}); err != nil {
		state.emitError("creating-volume", err)
		return fmt.Errorf("paired run: create volume: %w", err)
	}

	// 3. Create and start the engine container.
	state.emit(PairedRunEvent{Type: "status", Phase: "starting-engine", Source: "orchestrator"})
	engineID, err := m.createEngineContainer(ctx, spec, engineName, networkName)
	if err != nil {
		state.emitError("starting-engine", err)
		return fmt.Errorf("paired run: create engine: %w", err)
	}
	state.engineID = engineID

	if err := m.client.ContainerStart(ctx, engineID, containertypes.StartOptions{}); err != nil {
		state.emitError("starting-engine", err)
		return fmt.Errorf("paired run: start engine: %w", err)
	}

	// 4. Stream engine logs in the background.
	engineLogsCtx, cancelEngineLogs := context.WithCancel(ctx)
	defer cancelEngineLogs()
	go streamLogs(engineLogsCtx, m.client, engineID, "engine", state)

	// 5. Wait for engine health.
	state.emit(PairedRunEvent{Type: "status", Phase: "engine-starting", Source: "orchestrator"})
	engineIP, err := m.waitEngineHealthy(ctx, engineID, networkName, spec.Engine, state)
	if err != nil {
		state.emitError("engine-starting", err)
		return fmt.Errorf("paired run: engine health: %w", err)
	}
	state.emit(PairedRunEvent{Type: "status", Phase: "engine-healthy", Source: "orchestrator",
		Line: fmt.Sprintf("engine reachable at %s:%d", engineIP, spec.Engine.HealthCheck.Port)})

	// 6. Create and prep the benchmark container.
	state.emit(PairedRunEvent{Type: "status", Phase: "starting-benchmark", Source: "orchestrator"})
	benchID, err := m.createBenchmarkContainer(ctx, spec, benchName, networkName, volumeName, engineIP)
	if err != nil {
		state.emitError("starting-benchmark", err)
		return fmt.Errorf("paired run: create benchmark: %w", err)
	}
	state.benchID = benchID

	if err := m.copyBenchmarkConfig(ctx, benchID, spec.Benchmark); err != nil {
		state.emitError("starting-benchmark", err)
		return fmt.Errorf("paired run: copy benchmark config: %w", err)
	}

	if err := m.client.ContainerStart(ctx, benchID, containertypes.StartOptions{}); err != nil {
		state.emitError("starting-benchmark", err)
		return fmt.Errorf("paired run: start benchmark: %w", err)
	}

	benchLogsCtx, cancelBenchLogs := context.WithCancel(ctx)
	defer cancelBenchLogs()
	go streamLogs(benchLogsCtx, m.client, benchID, "benchmark", state)

	// 7. Wait for benchmark exit.
	state.emit(PairedRunEvent{Type: "status", Phase: "benchmark-running", Source: "orchestrator"})
	exitCode, waitErr := waitForExit(ctx, m.client, benchID)
	if waitErr != nil {
		state.emitError("benchmark-running", waitErr)
		return fmt.Errorf("paired run: wait benchmark: %w", waitErr)
	}

	if exitCode != 0 {
		state.emit(PairedRunEvent{Type: "error", Phase: "benchmark-exit", Source: "benchmark",
			Error: fmt.Sprintf("benchmark exited with code %d", exitCode)})
		// We still try to collect partial results.
	}

	// 8. Collect results.
	state.emit(PairedRunEvent{Type: "status", Phase: "collecting-results", Source: "orchestrator"})
	results, err := readResults(ctx, m.client, benchID)
	if err != nil {
		state.emitError("collecting-results", err)
		if exitCode != 0 {
			return fmt.Errorf("paired run: benchmark failed with exit code %d and no results", exitCode)
		}
		return fmt.Errorf("paired run: read results: %w", err)
	}

	duration := time.Since(start).Milliseconds()
	state.emit(PairedRunEvent{Type: "result", Phase: "done", Source: "orchestrator",
		Results: results, DurationMs: duration})

	return nil
}

// terminalEventTimeout is how long we block trying to deliver a "result" or
// "error" event before giving up. These events carry the run outcome and
// dropping them would leave SSE clients hanging with no conclusion.
const terminalEventTimeout = 10 * time.Second

// runState is shared between the orchestrator and log-streaming goroutines so
// cleanup knows which IDs to tear down. Progress events are emitted
// non-blockingly (dropped if the channel is full), but terminal events
// ("result" and "error") block with a timeout so the SSE client always
// receives the run outcome.
type runState struct {
	events   chan<- PairedRunEvent
	start    time.Time
	engineID string
	benchID  string
}

//nolint:gocritic // hugeParam: matches event-type struct layout.
func (s *runState) emit(ev PairedRunEvent) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}

	// Terminal events ("result", "error") must reach the client so the SSE
	// stream has a definitive outcome. We block with a timeout rather than
	// dropping silently. Progress events use non-blocking send — dropping
	// one is harmless since the next progress event supersedes it.
	if ev.Type == "result" || ev.Type == "error" {
		select {
		case s.events <- ev:
		case <-time.After(terminalEventTimeout):
			// Channel full for 10 s — the consumer is stuck or gone.
		}
		return
	}

	select {
	case s.events <- ev:
	default:
		// Progress event dropped; the client is not keeping up.
	}
}

func (s *runState) emitError(phase string, err error) {
	s.emit(PairedRunEvent{
		Type:   "error",
		Phase:  phase,
		Source: "orchestrator",
		Error:  err.Error(),
	})
}

// createEngineContainer creates (but doesn't start) the engine container with
// the model path bind-mounted, GPU injected, and connected to the run network.
//
//nolint:gocritic // hugeParam: spec passed by value to avoid surprising mutation.
func (m *Manager) createEngineContainer(ctx context.Context, spec PairedRunSpec, name, networkName string) (string, error) {
	cfg := &containertypes.Config{
		Image: spec.Engine.Image,
		Cmd:   spec.Engine.Args,
		Env:   envSlice(spec.Engine.Env),
	}

	hostCfg := &containertypes.HostConfig{
		Binds: []string{spec.Engine.ModelPath + ":" + modelMountPath + ":ro"},
	}
	InjectGPU(hostCfg, m.gpuVendor)

	netCfg := &networktypes.NetworkingConfig{
		EndpointsConfig: map[string]*networktypes.EndpointSettings{
			networkName: {},
		},
	}

	resp, err := m.client.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// createBenchmarkContainer creates the benchmark container with the results
// volume mounted and ENGINE_URL injected.
//
//nolint:gocritic // hugeParam: spec passed by value to avoid surprising mutation.
func (m *Manager) createBenchmarkContainer(ctx context.Context, spec PairedRunSpec, name, networkName, volumeName, engineIP string) (string, error) {
	env := map[string]string{}
	for k, v := range spec.Benchmark.Env {
		env[k] = v
	}
	env["ENGINE_URL"] = fmt.Sprintf("http://%s:%d", engineIP, spec.Engine.HealthCheck.Port)

	cfg := &containertypes.Config{
		Image: spec.Benchmark.Image,
		Cmd:   spec.Benchmark.Args,
		Env:   envSlice(env),
	}

	hostCfg := &containertypes.HostConfig{
		Mounts: []mounttypes.Mount{
			{
				Type:   mounttypes.TypeVolume,
				Source: volumeName,
				Target: resultsVolumeMount,
			},
		},
	}

	netCfg := &networktypes.NetworkingConfig{
		EndpointsConfig: map[string]*networktypes.EndpointSettings{
			networkName: {},
		},
	}

	resp, err := m.client.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// copyBenchmarkConfig stages the benchmark's YAML spec or config JSON into the
// benchmark container before it starts. For yaml benchmarks we write
// /config.yaml; for container benchmarks we write /config.json. Empty specs
// are skipped (the benchmark image may carry its own defaults).
//
//nolint:gocritic // hugeParam: spec is intentionally passed by value.
func (m *Manager) copyBenchmarkConfig(ctx context.Context, containerID string, spec BenchmarkSpec) error {
	var path string
	var body []byte
	switch {
	case spec.Kind == "yaml" && len(spec.YAMLSpec) > 0:
		path = "/config.yaml"
		body = spec.YAMLSpec
	case spec.Kind == "container" && len(spec.Config) > 0:
		path = "/config.json"
		body = spec.Config
	default:
		return nil
	}

	archive, err := tarWithSingleFile(path, body)
	if err != nil {
		return fmt.Errorf("build tar: %w", err)
	}
	return m.client.CopyToContainer(ctx, containerID, "/", archive, containertypes.CopyToContainerOptions{})
}

// waitEngineHealthy polls the engine's health endpoint via the run network
// until it returns 2xx or the timeout expires.
//
//nolint:gocritic // hugeParam: spec is intentionally passed by value.
func (m *Manager) waitEngineHealthy(ctx context.Context, engineID, networkName string, spec EngineSpec, state *runState) (string, error) {
	timeout := spec.HealthCheck.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultEngineHealthTimeout
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		info, err := m.client.ContainerInspect(ctx, engineID)
		if err == nil {
			ip := endpointIP(info, networkName)
			if ip != "" && spec.HealthCheck.Path != "" && spec.HealthCheck.Port != 0 {
				if checkHTTP(ctx, ip, spec.HealthCheck.Port, spec.HealthCheck.Path) {
					return ip, nil
				}
			} else if ip != "" && spec.HealthCheck.Path == "" {
				// No health path configured — consider the engine ready as
				// soon as it has an IP.
				return ip, nil
			}
		}
		state.emit(PairedRunEvent{Type: "progress", Phase: "engine-starting", Source: "engine",
			Line: "waiting for engine health"})

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", fmt.Errorf("engine did not become healthy within %ds", timeout)
}

// endpointIP returns the engine's IPv4 address on the run network.
func endpointIP(info containertypes.InspectResponse, networkName string) string {
	if info.NetworkSettings == nil {
		return ""
	}
	ep, ok := info.NetworkSettings.Networks[networkName]
	if !ok || ep == nil {
		return ""
	}
	return ep.IPAddress
}

// checkHTTP issues a GET against ip:port/path and returns true on 2xx.
func checkHTTP(ctx context.Context, ip string, port int, path string) bool {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := fmt.Sprintf("http://%s%s", net.JoinHostPort(ip, fmt.Sprint(port)), path)
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// streamLogs pipes a container's combined logs into PairedRunEvents. It stops
// when ctx is cancelled or the container stream ends.
func streamLogs(ctx context.Context, client DockerClient, containerID, source string, state *runState) {
	reader, err := client.ContainerLogs(ctx, containerID, containertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err != nil {
		state.emit(PairedRunEvent{Type: "error", Phase: "logs", Source: source, Error: err.Error()})
		return
	}
	defer func() { _ = reader.Close() }()

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, rerr := reader.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				i := bytes.IndexByte(buf, '\n')
				if i < 0 {
					break
				}
				line := strings.TrimRight(string(buf[:i]), "\r")
				buf = buf[i+1:]
				if line != "" {
					state.emit(PairedRunEvent{
						Type: "progress", Source: source, Line: line,
					})
				}
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) && !errors.Is(rerr, context.Canceled) {
				state.emit(PairedRunEvent{Type: "error", Phase: "logs", Source: source, Error: rerr.Error()})
			}
			return
		}
	}
}

// waitForExit blocks until the container exits and returns its exit code.
func waitForExit(ctx context.Context, client DockerClient, containerID string) (int64, error) {
	waitCh, errCh := client.ContainerWait(ctx, containerID, containertypes.WaitConditionNotRunning)
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case resp := <-waitCh:
		if resp.Error != nil && resp.Error.Message != "" {
			return resp.StatusCode, fmt.Errorf("wait: %s", resp.Error.Message)
		}
		return resp.StatusCode, nil
	case err := <-errCh:
		return 0, err
	}
}

// maxResultsSize is the upper bound on out.json before we reject it. This
// prevents a malicious benchmark container from OOM-ing the agent by writing
// an arbitrarily large results file.
const maxResultsSize = 256 * 1024 * 1024 // 256 MB

// readResults extracts /results/out.json from the benchmark container.
func readResults(ctx context.Context, client DockerClient, containerID string) (json.RawMessage, error) {
	path := resultsVolumeMount + "/" + resultsFile
	r, _, err := client.CopyFromContainer(ctx, containerID, path)
	if err != nil {
		return nil, fmt.Errorf("copy from container: %w", err)
	}
	defer func() { _ = r.Close() }()

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("results archive empty")
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Cap the read so a malicious benchmark can't OOM the agent.
		limited := io.LimitReader(tr, maxResultsSize+1)
		data, err := io.ReadAll(limited)
		if err != nil {
			return nil, fmt.Errorf("read results file: %w", err)
		}
		if len(data) > maxResultsSize {
			return nil, fmt.Errorf("results file exceeds %d bytes size limit", maxResultsSize)
		}
		if !json.Valid(data) {
			return nil, fmt.Errorf("results file is not valid JSON")
		}
		return data, nil
	}
}

// cleanupRun stops and removes both containers, the network, and the volume.
// Uses a background context so cleanup still runs when the caller's context
// was cancelled.
func (m *Manager) cleanupRun(ctx context.Context, networkName, volumeName string, state *runState) {
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, id := range []string{state.benchID, state.engineID} {
		if id == "" {
			continue
		}
		_ = m.client.ContainerStop(cleanupCtx, id, containertypes.StopOptions{})
		_ = m.client.ContainerRemove(cleanupCtx, id, containertypes.RemoveOptions{Force: true, RemoveVolumes: false})
	}
	if networkName != "" {
		_ = m.client.NetworkRemove(cleanupCtx, networkName)
	}
	if volumeName != "" {
		_ = m.client.VolumeRemove(cleanupCtx, volumeName, true)
	}
	m.logger.Debug("paired run cleanup complete",
		"network", networkName, "volume", volumeName,
		"engine", shortID(state.engineID), "benchmark", shortID(state.benchID))
}

// envSlice converts a map to Docker's "KEY=VAL" slice, producing a stable order.
func envSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// tarWithSingleFile returns a tar archive containing path with body.
func tarWithSingleFile(path string, body []byte) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: strings.TrimPrefix(path, "/"),
		Mode: 0o644,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if _, err := tw.Write(body); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}
