// Package operations provides the shared, authorized installation and recovery
// application services used by the CLI and Admin API.
package operations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Host is the privileged operating-system boundary used by operations services.
type Host interface {
	ReadFile(string) ([]byte, error)
	WriteFileAtomic(string, []byte, fs.FileMode) error
	Exists(string) (bool, error)
	MkdirAll(string, fs.FileMode) error
	Remove(string) error
	Run(context.Context, string, ...string) error
	GOOS() string
	GOARCH() string
}

// LocalHost performs exact-path operations beneath an explicit root.
type LocalHost struct{ root string }

// NewLocalHost constructs a host boundary. Use `/` for production and a
// dedicated temporary directory for tests or dry-run staging.
func NewLocalHost(root string) (*LocalHost, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("host root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &LocalHost{root: absolute}, nil
}
func (h *LocalHost) resolve(path string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) {
		volume := filepath.VolumeName(clean)
		clean = strings.TrimPrefix(clean, volume)
		clean = strings.TrimLeft(clean, string(filepath.Separator))
	}
	target := filepath.Join(h.root, clean)
	relative, err := filepath.Rel(h.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("operation path escapes host root")
	}
	return target, nil
}

// ReadFile reads one exact managed path.
func (h *LocalHost) ReadFile(path string) ([]byte, error) {
	target, err := h.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(target)
}

// WriteFileAtomic replaces one exact file through same-directory rename.
func (h *LocalHost) WriteFileAtomic(path string, payload []byte, mode fs.FileMode) error {
	target, err := h.resolve(path)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".aquaos-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err = file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err = file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporary, target); err != nil {
		return fmt.Errorf("activate file: %w", err)
	}
	return nil
}

// Exists reports whether one exact managed path exists.
func (h *LocalHost) Exists(path string) (bool, error) {
	target, err := h.resolve(path)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(target)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// MkdirAll creates one exact managed directory tree.
func (h *LocalHost) MkdirAll(path string, mode fs.FileMode) error {
	target, err := h.resolve(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(target, mode)
}

// Remove removes one exact file or empty directory, never recursively.
func (h *LocalHost) Remove(path string) error {
	target, err := h.resolve(path)
	if err != nil {
		return err
	}
	err = os.Remove(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Run executes an allow-listed host administration command only for `/`.
func (h *LocalHost) Run(ctx context.Context, name string, args ...string) error {
	if h.root != filepath.Clean(string(filepath.Separator)) {
		return nil
	}
	allowed := map[string]bool{"systemctl": true, "systemd-sysusers": true, "systemd-tmpfiles": true}
	if !allowed[name] {
		return errors.New("host command is not allow-listed")
	}
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// GOOS returns the host operating-system target.
func (*LocalHost) GOOS() string { return runtime.GOOS }

// GOARCH returns the host architecture target.
func (*LocalHost) GOARCH() string { return runtime.GOARCH }
