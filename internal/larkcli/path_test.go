package larkcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathPrefersConfiguredCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lark-cli")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LARK_CLI_PATH", path)
	resolved, err := ResolvePath()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != path {
		t.Fatalf("resolved %q, want %q", resolved, path)
	}
}

func TestResolvePathRejectsInvalidConfiguredCLI(t *testing.T) {
	t.Setenv("LARK_CLI_PATH", filepath.Join(t.TempDir(), "missing"))
	if _, err := ResolvePath(); err == nil {
		t.Fatal("expected invalid LARK_CLI_PATH to fail")
	}
}
