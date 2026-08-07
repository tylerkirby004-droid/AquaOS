package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStagingStatusNeedsNoProductionPrivilege(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--root", t.TempDir(), "status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"installed": false`) {
		t.Fatalf("output = %s", stdout.String())
	}
}

func TestVerifyArtifactCommandUsesReleaseSignaturePolicy(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binary := []byte("signed helper")
	digest := sha256.Sum256(binary)
	digestText := hex.EncodeToString(digest[:])
	signature := ed25519.Sign(privateKey, []byte(digestText))
	directory := t.TempDir()
	binaryPath := filepath.Join(directory, "helper")
	if err = os.WriteFile(binaryPath, binary, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--root", directory, "verify-artifact", "--binary", binaryPath, "--sha256", digestText, "--signature", hex.EncodeToString(signature), "--public-key", hex.EncodeToString(publicKey)}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), `"verify-artifact"`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
