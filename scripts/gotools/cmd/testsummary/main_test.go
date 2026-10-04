// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func runOn(t *testing.T, stream string) (int, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(p, []byte(stream), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"-name", "mod", p}, &out, &errOut)
	return code, out.String()
}

func TestRunPassing(t *testing.T) {
	code, out := runOn(t, `{"Action":"pass","Package":"p","Test":"TestA"}`+"\n"+`{"Action":"pass","Package":"p"}`+"\n")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := "go-test: mod: 1 packages (0 failed, 0 without tests), 1 tests: 1 passed, 0 skipped, 0 failed\n"
	if out != want {
		t.Errorf("output %q, want %q", out, want)
	}
}

func TestRunFailing(t *testing.T) {
	code, out := runOn(t, `{"Action":"fail","Package":"p","Test":"TestA"}`+"\n"+`{"Action":"fail","Package":"p"}`+"\n")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	want := "FAIL p TestA\ngo-test: mod: 1 packages (1 failed, 0 without tests), 1 tests: 0 passed, 0 skipped, 1 failed\n"
	if out != want {
		t.Errorf("output %q, want %q", out, want)
	}
}

func TestRunWithoutEvents(t *testing.T) {
	code, out := runOn(t, "go: no Go files\n")
	if code != 1 || out != "go-test: mod: tests did not run, 1 failed\n" {
		t.Errorf("exit %d, output %q", code, out)
	}
}

func TestRunUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if code := run([]string{filepath.Join(t.TempDir(), "missing.json")}, &out, &errOut); code != 2 {
		t.Errorf("exit %d for a missing file, want 2", code)
	}
}
