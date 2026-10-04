// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// module builds a small module with one changed file, one generated file, one test file and one
// untracked file, plus the profile and diff that go-test.sh would pass in.
func module(t *testing.T) (dir string, args []string) {
	t.Helper()
	dir = t.TempDir()
	write(t, dir, "go.mod", "module example.com/m\n\ngo 1.26\n")
	write(t, dir, "p/a.go", "package p\n")
	write(t, dir, "p/a_test.go", "package p\n")
	write(t, dir, "p/mock.go", "// Code generated from mocks.yaml; DO NOT EDIT.\n\npackage p\n")
	write(t, dir, "p/new.go", "package p\n")
	profile := write(t, dir, "out/cover.out", "mode: set\n"+
		"example.com/m/p/a.go:3.1,5.2 2 1\n"+
		"example.com/m/p/a.go:7.1,9.2 2 0\n"+
		"example.com/m/p/mock.go:3.1,4.2 5 0\n"+
		"example.com/m/p/new.go:3.1,4.2 1 1\n")
	diff := write(t, dir, "out/changes.diff", ""+
		"--- a/p/a.go\n+++ b/p/a.go\n@@ -4 +4 @@\n-x\n+y\n@@ -8 +8 @@\n-x\n+y\n"+
		"--- a/p/a_test.go\n+++ b/p/a_test.go\n@@ -1 +1 @@\n-x\n+y\n"+
		"--- a/p/mock.go\n+++ b/p/mock.go\n@@ -3 +3 @@\n-x\n+y\n")
	untracked := write(t, dir, "out/untracked.txt", "p/new.go\n")
	return dir, []string{"-module", dir, "-profile", profile, "-diff", diff, "-untracked", untracked}
}

func TestRunReportsChangedStatements(t *testing.T) {
	_, args := module(t)
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	want := "changed-cover: p/a.go: 2/4 statements (50.0%)\n" +
		"changed-cover: p/new.go: 1/1 statements (100.0%)\n" +
		"changed-cover: total 3/5 statements on changed lines (60.0%) in 2 files\n"
	if out.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestRunFailsBelowMinimum(t *testing.T) {
	_, args := module(t)
	var out, errOut bytes.Buffer
	if code := run(append(args, "-min", "75"), &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1; output %q", code, out.String())
	}
	if !strings.Contains(out.String(), "below the minimum of 75.0%") {
		t.Errorf("output %q does not name the minimum", out.String())
	}
}

func TestRunNeedsProfileAndDiff(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-module", "."}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRunWithoutChangedGoFiles(t *testing.T) {
	dir, _ := module(t)
	empty := write(t, dir, "out/empty.diff", "")
	var out, errOut bytes.Buffer
	args := []string{"-module", dir, "-profile", filepath.Join(dir, "out", "cover.out"), "-diff", empty}
	if code := run(args, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if out.String() != "changed-cover: no changed Go files\n" {
		t.Errorf("output %q", out.String())
	}
}

func TestRunRejectsModuleWithoutGoMod(t *testing.T) {
	dir, args := module(t)
	if err := os.Remove(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
