package operations

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"testing"

	"github.com/tylerkirby004-droid/aquaos/internal/config"
)

type fakeHost struct {
	files       map[string][]byte
	dirs        map[string]bool
	commands    []string
	failWrite   string
	failCommand string
	goos        string
	goarch      string
}

func newFakeHost() *fakeHost {
	return &fakeHost{files: make(map[string][]byte), dirs: make(map[string]bool), goos: "linux", goarch: "amd64"}
}
func (f *fakeHost) ReadFile(path string) ([]byte, error) {
	value, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), value...), nil
}
func (f *fakeHost) WriteFileAtomic(path string, payload []byte, _ fs.FileMode) error {
	if path == f.failWrite {
		return errors.New("injected atomic write failure")
	}
	f.files[path] = append([]byte(nil), payload...)
	return nil
}
func (f *fakeHost) Exists(path string) (bool, error) {
	_, file := f.files[path]
	return file || f.dirs[path], nil
}
func (f *fakeHost) MkdirAll(path string, _ fs.FileMode) error { f.dirs[path] = true; return nil }
func (f *fakeHost) Remove(path string) error                  { delete(f.files, path); delete(f.dirs, path); return nil }
func (f *fakeHost) Run(_ context.Context, name string, args ...string) error {
	command := name
	for _, arg := range args {
		command += " " + arg
	}
	f.commands = append(f.commands, command)
	if f.failCommand != "" && command == f.failCommand {
		return errors.New("injected command failure")
	}
	return nil
}
func (f *fakeHost) GOOS() string   { return f.goos }
func (f *fakeHost) GOARCH() string { return f.goarch }
func validConfiguration(t *testing.T) []byte {
	t.Helper()
	payload := []byte("schema_version: 1\n")
	if _, err := config.DecodeCandidate(payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
func newService(t *testing.T, host Host) *Service {
	t.Helper()
	service, err := New(host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

var administrator = Actor{ID: "root-test", Administrator: true}

func TestGetConfigurationReturnsValidatedSnapshot(t *testing.T) {
	host := newFakeHost()
	host.files[configPath] = []byte("schema_version: 1\nhttp:\n  bearer_token_file: /etc/aquaos/secrets/api.token\n")
	value, err := newService(t, host).GetConfiguration(context.Background(), administrator)
	if err != nil {
		t.Fatal(err)
	}
	if value.SchemaVersion != config.CurrentSchemaVersion || value.HTTP.BearerTokenFile != "/etc/aquaos/secrets/api.token" {
		t.Fatalf("configuration = %+v", value)
	}
}

func signInstall(t *testing.T, request InstallRequest) InstallRequest {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(request.Binary)
	request.SHA256 = hex.EncodeToString(sum[:])
	request.PublicKey = public
	request.Signature = ed25519.Sign(private, []byte(request.SHA256))
	return request
}

func TestInstallIsIdempotentAndPreservesExistingConfigAndUnrelatedFiles(t *testing.T) {
	host := newFakeHost()
	host.files["/etc/aquaos/aquaos.yaml"] = validConfiguration(t)
	host.files["/etc/example/unrelated.conf"] = []byte("preserve")
	service := newService(t, host)
	request := signInstall(t, InstallRequest{Actor: administrator, Version: "v0.8.0", Binary: []byte("binary"), Configuration: []byte("schema_version: 1\napplication:\n  log_level: debug\n"), ControlVMAcknowledged: true})
	first, err := service.Install(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Install(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || second.Changed {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if string(host.files["/etc/aquaos/aquaos.yaml"]) != "schema_version: 1\n" {
		t.Fatal("existing configuration overwritten")
	}
	if string(host.files["/etc/example/unrelated.conf"]) != "preserve" {
		t.Fatal("unrelated file changed")
	}
}

func TestInterruptedInstallDoesNotReplaceExistingBinary(t *testing.T) {
	host := newFakeHost()
	host.files[binaryPath] = []byte("old")
	host.failWrite = binaryPath
	service := newService(t, host)
	_, err := service.Install(context.Background(), signInstall(t, InstallRequest{Actor: administrator, Version: "v0.8.0", Binary: []byte("new"), Configuration: validConfiguration(t), ControlVMAcknowledged: true}))
	if err == nil {
		t.Fatal("injected failure ignored")
	}
	if string(host.files[binaryPath]) != "old" {
		t.Fatal("existing binary was corrupted")
	}
}

func TestInstallRejectsHypervisorHost(t *testing.T) {
	host := newFakeHost()
	host.dirs["/etc/pve"] = true
	service := newService(t, host)
	_, err := service.Install(context.Background(), signInstall(t, InstallRequest{Actor: administrator, Version: "v0.8.0", Binary: []byte("binary"), Configuration: validConfiguration(t), ControlVMAcknowledged: true}))
	if err == nil {
		t.Fatal("hypervisor host was accepted")
	}
}

func TestFailedUpgradeRollsBackPreviousBinary(t *testing.T) {
	host := newFakeHost()
	host.files[binaryPath] = []byte("old")
	host.files[versionPath] = []byte("v0.7\n")
	host.failCommand = "systemctl restart aquaos.service"
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binary := []byte("new")
	sum := sha256.Sum256(binary)
	digest := hex.EncodeToString(sum[:])
	signature := ed25519.Sign(private, []byte(digest))
	result, err := newService(t, host).Upgrade(context.Background(), UpgradeRequest{Actor: administrator, Version: "v0.8", Binary: binary, SHA256: digest, Signature: signature, PublicKey: public})
	if err == nil || !result.RolledBack {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if string(host.files[binaryPath]) != "old" || string(host.files[versionPath]) != "v0.7\n" {
		t.Fatal("failed upgrade did not restore previous release")
	}
}

func TestBackupRestoresReplacementHost(t *testing.T) {
	source := newFakeHost()
	source.files[configPath] = validConfiguration(t)
	source.files[versionPath] = []byte("v0.8\n")
	archive, err := newService(t, source).Backup(context.Background(), administrator)
	if err != nil {
		t.Fatal(err)
	}
	replacement := newFakeHost()
	result, err := newService(t, replacement).Restore(context.Background(), administrator, archive, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || string(replacement.files[configPath]) != "schema_version: 1\n" || string(replacement.files[versionPath]) != "v0.8\n" {
		t.Fatalf("result=%+v files=%v", result, replacement.files)
	}
}

func TestUnauthorizedOperationsAreRejected(t *testing.T) {
	_, err := newService(t, newFakeHost()).GetStatus(context.Background(), Actor{ID: "viewer"})
	if err == nil {
		t.Fatal("unauthorized status was returned")
	}
}

func TestFailedConfigurationActivationRollsBack(t *testing.T) {
	host := newFakeHost()
	host.files[configPath] = validConfiguration(t)
	host.failCommand = "systemctl restart aquaos.service"
	candidate := []byte("schema_version: 1\napplication:\n  log_level: debug\n")
	result, err := newService(t, host).ApplyConfiguration(context.Background(), administrator, candidate, false)
	if err == nil || !result.RolledBack {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if string(host.files[configPath]) != "schema_version: 1\n" {
		t.Fatal("failed configuration was not rolled back")
	}
}

func TestPackagedSystemdUnitMatchesInstaller(t *testing.T) {
	payload, err := os.ReadFile("../../packaging/systemd/aquaos.service")
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != systemdUnit {
		t.Fatal("packaged and installed systemd units differ")
	}
}
