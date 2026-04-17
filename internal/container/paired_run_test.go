package container_test

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
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	volumetypes "github.com/docker/docker/api/types/volume"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/container"
	"github.com/flag-ai/bonnie/internal/gpu"
)

// pairedRunMock is a richer mock than the manager_test mockDockerClient. It
// records the sequence of docker operations and can simulate an engine that
// becomes healthy after N inspect calls.
type pairedRunMock struct {
	mu sync.Mutex

	// Fake engine http server used when HealthCheck.Path != "".
	engineHealth *httptest.Server

	createdContainers []string
	startedContainers []string
	stoppedContainers []string
	removedContainers []string

	networksCreated []string
	networksRemoved []string
	volumesCreated  []string
	volumesRemoved  []string

	copyToPayloads map[string][]byte // containerID -> payload bytes

	// benchmarkExitCode is returned from ContainerWait for the benchmark.
	benchmarkExitCode int64

	// resultsJSON, if non-nil, is returned via CopyFromContainer.
	resultsJSON []byte

	// logs per container id returned from ContainerLogs.
	logs map[string]string

	// waitBlock, if non-nil, is used to gate ContainerWait so callers can
	// cancel the context in the middle of a run.
	waitBlock chan struct{}

	// Counters so tests can assert idempotent cleanup even on error paths.
	inspectCount atomic.Int64

	nextContainerID atomic.Int64
}

func newPairedRunMock() *pairedRunMock {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return &pairedRunMock{
		engineHealth:   httptest.NewServer(mux),
		copyToPayloads: map[string][]byte{},
		logs:           map[string]string{},
	}
}

func (m *pairedRunMock) close() {
	if m.engineHealth != nil {
		m.engineHealth.Close()
	}
}

func (m *pairedRunMock) healthHostPort() (ip string, port int) {
	u, _ := net.ResolveTCPAddr("tcp", strings.TrimPrefix(m.engineHealth.URL, "http://"))
	return u.IP.String(), u.Port
}

func (m *pairedRunMock) ContainerCreate(_ context.Context, _ *containertypes.Config, _ *containertypes.HostConfig, _ *networktypes.NetworkingConfig, _ *ocispec.Platform, name string) (containertypes.CreateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := fmt.Sprintf("cid-%d-%s", m.nextContainerID.Add(1), name)
	m.createdContainers = append(m.createdContainers, id)
	return containertypes.CreateResponse{ID: id}, nil
}

func (m *pairedRunMock) ContainerStart(_ context.Context, id string, _ containertypes.StartOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startedContainers = append(m.startedContainers, id)
	return nil
}

func (m *pairedRunMock) ContainerStop(_ context.Context, id string, _ containertypes.StopOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stoppedContainers = append(m.stoppedContainers, id)
	return nil
}

func (m *pairedRunMock) ContainerRestart(_ context.Context, _ string, _ containertypes.StopOptions) error {
	return nil
}

func (m *pairedRunMock) ContainerRemove(_ context.Context, id string, _ containertypes.RemoveOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removedContainers = append(m.removedContainers, id)
	return nil
}

func (m *pairedRunMock) ContainerInspect(_ context.Context, id string) (containertypes.InspectResponse, error) {
	m.inspectCount.Add(1)
	// Always report the test HTTP server's IP on whatever network the engine
	// is in; the test's HealthCheck uses the same port.
	ip, _ := m.healthHostPort()
	return containertypes.InspectResponse{
		ContainerJSONBase: &containertypes.ContainerJSONBase{ID: id},
		NetworkSettings: &containertypes.NetworkSettings{
			Networks: map[string]*networktypes.EndpointSettings{
				// We don't know the network name at inspect time; put the IP
				// under a catch-all key that endpointIP will match.
				m.lastNetworkName(): {IPAddress: ip},
			},
		},
	}, nil
}

func (m *pairedRunMock) lastNetworkName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.networksCreated) == 0 {
		return ""
	}
	return m.networksCreated[len(m.networksCreated)-1]
}

func (m *pairedRunMock) ContainerList(_ context.Context, _ containertypes.ListOptions) ([]containertypes.Summary, error) {
	return nil, nil
}

func (m *pairedRunMock) ContainerLogs(ctx context.Context, id string, _ containertypes.LogsOptions) (io.ReadCloser, error) {
	m.mu.Lock()
	logs, ok := m.logs[id]
	m.mu.Unlock()
	if !ok {
		// Return an empty reader that blocks until ctx is done so the goroutine
		// shuts down cleanly.
		pr, pw := io.Pipe()
		go func() {
			<-ctx.Done()
			_ = pw.Close()
		}()
		return pr, nil
	}
	return io.NopCloser(strings.NewReader(logs)), nil
}

func (m *pairedRunMock) ContainerWait(ctx context.Context, _ string, _ containertypes.WaitCondition) (respCh <-chan containertypes.WaitResponse, errCh <-chan error) {
	resp := make(chan containertypes.WaitResponse, 1)
	errs := make(chan error, 1)
	go func() {
		if m.waitBlock != nil {
			select {
			case <-m.waitBlock:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
		resp <- containertypes.WaitResponse{StatusCode: m.benchmarkExitCode}
	}()
	respCh = resp
	errCh = errs
	return respCh, errCh
}

func (m *pairedRunMock) CopyToContainer(_ context.Context, id, _ string, content io.Reader, _ containertypes.CopyToContainerOptions) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.copyToPayloads[id] = data
	m.mu.Unlock()
	return nil
}

func (m *pairedRunMock) CopyFromContainer(_ context.Context, _, _ string) (io.ReadCloser, containertypes.PathStat, error) {
	if m.resultsJSON == nil {
		return nil, containertypes.PathStat{}, fmt.Errorf("no results")
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{
		Name: "out.json",
		Mode: 0o644,
		Size: int64(len(m.resultsJSON)),
	})
	_, _ = tw.Write(m.resultsJSON)
	_ = tw.Close()
	return io.NopCloser(&buf), containertypes.PathStat{}, nil
}

//nolint:gocritic // hugeParam: signature matches DockerClient interface.
func (m *pairedRunMock) NetworkCreate(_ context.Context, name string, _ networktypes.CreateOptions) (networktypes.CreateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.networksCreated = append(m.networksCreated, name)
	return networktypes.CreateResponse{ID: "net-" + name}, nil
}

func (m *pairedRunMock) NetworkRemove(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.networksRemoved = append(m.networksRemoved, id)
	return nil
}

func (m *pairedRunMock) VolumeCreate(_ context.Context, opts volumetypes.CreateOptions) (volumetypes.Volume, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.volumesCreated = append(m.volumesCreated, opts.Name)
	return volumetypes.Volume{Name: opts.Name}, nil
}

func (m *pairedRunMock) VolumeRemove(_ context.Context, id string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.volumesRemoved = append(m.volumesRemoved, id)
	return nil
}

func (m *pairedRunMock) ImagePull(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *pairedRunMock) Ping(_ context.Context) (types.Ping, error) {
	return types.Ping{}, nil
}

func (m *pairedRunMock) Close() error { return nil }

// makeSpec builds a PairedRunSpec whose health check points at the mock's
// fake HTTP server.
func makeSpec(m *pairedRunMock) container.PairedRunSpec {
	_, port := m.healthHostPort()
	return container.PairedRunSpec{
		RunID: "unit-run",
		Engine: container.EngineSpec{
			Image:     "engine:v1",
			Args:      []string{"--model", "/model"},
			Env:       map[string]string{"FOO": "bar"},
			ModelPath: "/var/lib/bonnie/models/x",
			HealthCheck: container.HealthCheck{
				Path:           "/health",
				Port:           port,
				TimeoutSeconds: 5,
			},
		},
		Benchmark: container.BenchmarkSpec{
			Kind:   "container",
			Image:  "bench:v1",
			Args:   []string{"--engine-url", "http://engine:8000"},
			Config: json.RawMessage(`{"prompts": 10}`),
		},
	}
}

func collectEvents(t *testing.T, events <-chan container.PairedRunEvent) []container.PairedRunEvent {
	t.Helper()
	var out []container.PairedRunEvent
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func TestPairedRun_HappyPath(t *testing.T) {
	t.Parallel()

	m := newPairedRunMock()
	t.Cleanup(m.close)
	m.resultsJSON = []byte(`{"accuracy":0.9}`)

	mgr := container.NewManager(m, gpu.VendorNVIDIA, newTestLogger())
	spec := makeSpec(m)

	events := make(chan container.PairedRunEvent, 64)
	var runErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = mgr.PairedRun(context.Background(), spec, events)
	}()

	evs := collectEvents(t, events)
	wg.Wait()

	require.NoError(t, runErr)

	// Collect event types + phases in order.
	var phases []string
	var haveResult bool
	for _, ev := range evs {
		if ev.Phase != "" {
			phases = append(phases, ev.Type+":"+ev.Phase)
		}
		if ev.Type == "result" {
			haveResult = true
			assert.JSONEq(t, `{"accuracy":0.9}`, string(ev.Results))
		}
	}
	assert.True(t, haveResult, "expected result event")
	assert.Contains(t, phases, "status:creating-network")
	assert.Contains(t, phases, "status:creating-volume")
	assert.Contains(t, phases, "status:engine-healthy")
	assert.Contains(t, phases, "status:starting-benchmark")
	assert.Contains(t, phases, "result:done")

	// Verify config was written.
	m.mu.Lock()
	defer m.mu.Unlock()
	require.Len(t, m.createdContainers, 2, "expected engine + benchmark containers")
	require.Len(t, m.networksCreated, 1)
	require.Len(t, m.volumesCreated, 1)
	// Both containers were stopped and removed in cleanup.
	require.Len(t, m.removedContainers, 2)
	require.Len(t, m.networksRemoved, 1)
	require.Len(t, m.volumesRemoved, 1)

	// Config payload: tar contains config.json with the request body.
	benchID := m.createdContainers[1]
	payload, ok := m.copyToPayloads[benchID]
	require.True(t, ok, "benchmark config should have been copied")
	assert.True(t, containsTarFile(t, payload, "config.json", `{"prompts": 10}`))
}

func TestPairedRun_YAMLConfig(t *testing.T) {
	t.Parallel()

	m := newPairedRunMock()
	t.Cleanup(m.close)
	m.resultsJSON = []byte(`{"pass_rate":0.5}`)

	mgr := container.NewManager(m, gpu.VendorUnknown, newTestLogger())
	spec := makeSpec(m)
	spec.Benchmark.Kind = "yaml"
	spec.Benchmark.YAMLSpec = json.RawMessage(`{"dataset":"mmlu"}`)
	spec.Benchmark.Config = nil

	events := make(chan container.PairedRunEvent, 64)
	err := mgr.PairedRun(context.Background(), spec, events)
	require.NoError(t, err)
	for range events { //nolint:revive // drain
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	benchID := m.createdContainers[1]
	payload := m.copyToPayloads[benchID]
	assert.True(t, containsTarFile(t, payload, "config.yaml", `{"dataset":"mmlu"}`))
}

func TestPairedRun_NonZeroExitStillReturnsResults(t *testing.T) {
	t.Parallel()

	m := newPairedRunMock()
	t.Cleanup(m.close)
	m.benchmarkExitCode = 2
	m.resultsJSON = []byte(`{"partial":true}`)

	mgr := container.NewManager(m, gpu.VendorUnknown, newTestLogger())
	spec := makeSpec(m)

	events := make(chan container.PairedRunEvent, 64)
	err := mgr.PairedRun(context.Background(), spec, events)
	require.NoError(t, err)

	var haveError, haveResult bool
	for ev := range events {
		if ev.Type == "error" && ev.Source == "benchmark" {
			haveError = true
		}
		if ev.Type == "result" {
			haveResult = true
			assert.JSONEq(t, `{"partial":true}`, string(ev.Results))
		}
	}
	assert.True(t, haveError)
	assert.True(t, haveResult)
}

func TestPairedRun_CleanupOnContextCancel(t *testing.T) {
	t.Parallel()

	m := newPairedRunMock()
	t.Cleanup(m.close)
	m.waitBlock = make(chan struct{}) // benchmark never exits on its own
	m.resultsJSON = []byte(`{}`)

	mgr := container.NewManager(m, gpu.VendorUnknown, newTestLogger())
	spec := makeSpec(m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan container.PairedRunEvent, 64)

	var runErr error
	done := make(chan struct{})
	go func() {
		runErr = mgr.PairedRun(ctx, spec, events)
		close(done)
	}()

	// Drain events until we see the benchmark start; then cancel.
	cancelled := false
	go func() {
		for ev := range events {
			if !cancelled && ev.Phase == "benchmark-running" {
				cancel()
				cancelled = true
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not exit after cancel")
	}

	require.Error(t, runErr)

	m.mu.Lock()
	defer m.mu.Unlock()
	// Cleanup must still have stopped + removed both containers and
	// destroyed the network+volume even though the context was cancelled.
	require.Len(t, m.removedContainers, 2)
	require.Len(t, m.networksRemoved, 1)
	require.Len(t, m.volumesRemoved, 1)
}

func TestPairedRun_ValidatesSpec(t *testing.T) {
	t.Parallel()

	m := newPairedRunMock()
	t.Cleanup(m.close)
	mgr := container.NewManager(m, gpu.VendorUnknown, newTestLogger())

	cases := []struct {
		name string
		spec container.PairedRunSpec
		want string
	}{
		{
			name: "missing run_id",
			spec: container.PairedRunSpec{
				Engine:    container.EngineSpec{Image: "e"},
				Benchmark: container.BenchmarkSpec{Image: "b"},
			},
			want: "run_id",
		},
		{
			name: "invalid run_id characters",
			spec: container.PairedRunSpec{
				RunID:     "r/../evil",
				Engine:    container.EngineSpec{Image: "e"},
				Benchmark: container.BenchmarkSpec{Image: "b"},
			},
			want: "invalid",
		},
		{
			name: "missing engine image",
			spec: container.PairedRunSpec{
				RunID:     "r",
				Benchmark: container.BenchmarkSpec{Image: "b"},
			},
			want: "engine.image",
		},
		{
			name: "missing benchmark image",
			spec: container.PairedRunSpec{
				RunID:  "r",
				Engine: container.EngineSpec{Image: "e"},
			},
			want: "benchmark.image",
		},
		{
			name: "invalid benchmark kind",
			spec: container.PairedRunSpec{
				RunID:     "r",
				Engine:    container.EngineSpec{Image: "e"},
				Benchmark: container.BenchmarkSpec{Image: "b", Kind: "weird"},
			},
			want: "benchmark.kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			events := make(chan container.PairedRunEvent, 8)
			err := mgr.PairedRun(context.Background(), tc.spec, events)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// containsTarFile decodes a tar archive and returns true if it contains the
// given filename with the given body.
func containsTarFile(t *testing.T, data []byte, name, body string) bool {
	t.Helper()
	if len(data) == 0 {
		return false
	}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return false
		}
		require.NoError(t, err)
		if hdr.Name != name && hdr.Name != "/"+name {
			continue
		}
		got, err := io.ReadAll(tr)
		require.NoError(t, err)
		return string(got) == body
	}
}

// Quick sanity: the mock server responds on /health.
func TestPairedRunMock_FakeEngineHealth(t *testing.T) {
	t.Parallel()

	m := newPairedRunMock()
	t.Cleanup(m.close)
	ip, port := m.healthHostPort()
	resp, err := http.Get("http://" + net.JoinHostPort(ip, strconv.Itoa(port)) + "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
