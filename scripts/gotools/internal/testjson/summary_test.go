// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

package testjson

import (
	"reflect"
	"strings"
	"testing"
)

func TestSummarizeCountsPackagesAndTests(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"start","Package":"example.com/a"}`,
		`{"Action":"run","Package":"example.com/a","Test":"TestOne"}`,
		`{"Action":"pass","Package":"example.com/a","Test":"TestOne"}`,
		`{"Action":"run","Package":"example.com/a","Test":"TestSuite"}`,
		`{"Action":"pass","Package":"example.com/a","Test":"TestSuite/TestMethod"}`,
		`{"Action":"skip","Package":"example.com/a","Test":"TestSuite/TestSkipped"}`,
		`{"Action":"pass","Package":"example.com/a","Test":"TestSuite"}`,
		`{"Action":"pass","Package":"example.com/a"}`,
		`{"Action":"output","Package":"example.com/b","Output":"?   \texample.com/b\t[no test files]\n"}`,
		`{"Action":"skip","Package":"example.com/b"}`,
	}, "\n")

	got, err := Summarize(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	want := Summary{Packages: 2, PackagesNoTest: 1, TestsPassed: 3, TestsSkipped: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if line := got.Line(); !strings.HasSuffix(line, " 0 failed") {
		t.Errorf("summary line %q must end with \" 0 failed\"", line)
	}
}

func TestSummarizeReportsFailedTestsOnce(t *testing.T) {
	// A failed subtest also fails its parent; both are reported, and the package failure is not
	// counted again because a test explains it.
	in := strings.Join([]string{
		`{"Action":"fail","Package":"example.com/a","Test":"TestSuite/TestBroken"}`,
		`{"Action":"fail","Package":"example.com/a","Test":"TestSuite"}`,
		`{"Action":"fail","Package":"example.com/a"}`,
	}, "\n")

	got, err := Summarize(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	wantFailed := []string{"example.com/a TestSuite", "example.com/a TestSuite/TestBroken"}
	if !reflect.DeepEqual(got.Failed, wantFailed) {
		t.Errorf("Failed = %v, want %v", got.Failed, wantFailed)
	}
	if got.PackagesFailed != 1 || got.Failures() != 2 {
		t.Errorf("PackagesFailed = %d, Failures = %d; want 1 and 2", got.PackagesFailed, got.Failures())
	}
}

func TestSummarizeCountsBuildFailures(t *testing.T) {
	in := strings.Join([]string{
		"# example.com/c",
		`{"ImportPath":"example.com/c","Action":"build-output","Output":"c.go:3:1: syntax error\n"}`,
		`{"ImportPath":"example.com/c","Action":"build-fail"}`,
		`{"Action":"start","Package":"example.com/c"}`,
		`{"Action":"fail","Package":"example.com/c","FailedBuild":"example.com/c"}`,
	}, "\n")

	got, err := Summarize(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got.Failures() != 1 || got.Failed[0] != "example.com/c" {
		t.Fatalf("got %+v, want one failed package example.com/c", got)
	}
	if !strings.HasSuffix(got.Line(), " 1 failed") {
		t.Errorf("summary line %q must end with \" 1 failed\"", got.Line())
	}
}

func TestSummarizeRejectsEmptyStream(t *testing.T) {
	if _, err := Summarize(strings.NewReader("go: cannot find main module\n")); err == nil {
		t.Fatal("expected an error for a stream without test events")
	}
}
