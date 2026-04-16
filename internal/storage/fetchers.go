package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// huggingfaceFetcher stages models by shelling out to `huggingface-cli download`.
type huggingfaceFetcher struct {
	token  string
	logger *slog.Logger
}

// Fetch implements Fetcher by invoking huggingface-cli with the caller's
// patterns and optional HF token.
//
//nolint:gocritic // hugeParam: signature matches Fetcher interface.
func (f *huggingfaceFetcher) Fetch(ctx context.Context, req FetchRequest, dest string) ([]string, error) {
	bin, err := exec.LookPath("huggingface-cli")
	if err != nil {
		return nil, fmt.Errorf("huggingface-cli not found on PATH: %w", err)
	}

	args := []string{
		"download", req.ModelID,
		"--local-dir", dest,
		"--local-dir-use-symlinks=False",
	}
	for _, p := range req.Patterns {
		if p == "" {
			continue
		}
		args = append(args, "--include", p)
	}

	// #nosec G204 -- args are constructed from validated struct fields and
	// the binary path comes from exec.LookPath.
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "HF_HUB_DISABLE_TELEMETRY=1")
	if f.token != "" {
		cmd.Env = append(cmd.Env, "HF_TOKEN="+f.token, "HUGGING_FACE_HUB_TOKEN="+f.token)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("huggingface-cli download: %w: %s", err, string(out))
	}

	f.logger.Debug("huggingface-cli completed",
		"model_id", req.ModelID, "dest", dest, "output_bytes", len(out))

	// Let the store re-scan the directory.
	return nil, nil
}

// nfsFetcher copies model files from a pre-mounted NFS share.
type nfsFetcher struct {
	logger *slog.Logger
}

// Fetch implements Fetcher by copying req.MountSource/req.Subpath (file or
// directory) into dest.
//
//nolint:gocritic // hugeParam: signature matches Fetcher interface.
func (f *nfsFetcher) Fetch(_ context.Context, req FetchRequest, dest string) ([]string, error) {
	src := filepath.Join(req.MountSource, req.Subpath)
	// #nosec G304 -- src is built from caller-supplied mount_source/subpath
	// which are already validated by the store.
	info, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("nfs source %q: %w", src, err)
	}

	if !info.IsDir() {
		if err := copyFile(src, filepath.Join(dest, filepath.Base(src))); err != nil {
			return nil, fmt.Errorf("nfs copy file: %w", err)
		}
		return []string{filepath.Base(src)}, nil
	}

	if err := copyTree(src, dest); err != nil {
		return nil, fmt.Errorf("nfs copy tree: %w", err)
	}
	f.logger.Debug("nfs copy complete", "src", src, "dest", dest)
	// Store will rescan the tree to build the file list.
	return nil, nil
}

// copyFile copies a single file, preserving mode (bounded by 0o640 on writes
// so private tokens in filenames aren't world-readable).
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	// #nosec G304 -- src/dst are derived from validated paths.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	mode := info.Mode() & 0o640
	if mode == 0 {
		mode = 0o640
	}
	// #nosec G304 -- dst built from caller-validated paths.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// copyTree recursively copies src into dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o750)
		case d.Type().IsRegular():
			return copyFile(path, target)
		case d.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// Skip if destination exists already (idempotent copy).
			if _, err := os.Lstat(target); err == nil {
				return nil
			}
			// #nosec G122 -- we're copying a tree the caller already owns;
			// the symlink target is taken from the source tree verbatim.
			return os.Symlink(link, target)
		default:
			return errors.New("unsupported file type: " + path)
		}
	})
}
