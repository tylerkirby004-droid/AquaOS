package simulator

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSimulatorProductionFilesCannotImportHardwareOrNetworkPackages(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"net", "net/http", "os/exec", "syscall", "/adapters/shelly", "/adapters/esp32", "periph.io/", "go.bug.st/serial"}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, denied := range forbidden {
				if name == denied || strings.Contains(name, denied) {
					t.Fatalf("%s imports forbidden hardware/network package %q", path, name)
				}
			}
		}
	}
}
