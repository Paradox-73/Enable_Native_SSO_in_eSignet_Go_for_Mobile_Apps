// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Package testjson summarises the event stream written by "go test -json" (cmd/test2json).
package testjson

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// event is one line of "go test -json" output. Only the fields the summary needs are decoded.
type event struct {
	Action  string
	Package string
	Test    string
	Output  string
	// ImportPath is set on build-output and build-fail events (Go 1.24 and later) instead of Package.
	ImportPath string
}

// Summary counts the final result of every package and every test (subtests included).
type Summary struct {
	Packages       int // packages that reported a result
	PackagesFailed int
	PackagesNoTest int // packages without test files
	TestsPassed    int
	TestsFailed    int
	TestsSkipped   int
	// Failed lists "package TestName" for each failed test and "package" for each package that
	// failed without a failed test (build failure, panic in TestMain, timeout), sorted.
	Failed []string
}

// Failures is the number the one-line summary ends with: failed tests plus packages that failed
// without any failed test, so a package that does not build is never reported as "0 failed".
func (s Summary) Failures() int { return len(s.Failed) }

// Line renders the one-line summary, for example
// "29 packages (0 failed, 2 without tests), 812 tests: 805 passed, 7 skipped, 0 failed".
func (s Summary) Line() string {
	return fmt.Sprintf("%d packages (%d failed, %d without tests), %d tests: %d passed, %d skipped, %d failed",
		s.Packages, s.PackagesFailed, s.PackagesNoTest,
		s.TestsPassed+s.TestsFailed+s.TestsSkipped, s.TestsPassed, s.TestsSkipped, s.Failures())
}

// Summarize reads a "go test -json" stream. Lines that are not JSON (for example a go command
// error printed before the stream starts) are ignored; a stream with no events is an error,
// because it means the tests never ran.
func Summarize(r io.Reader) (Summary, error) {
	type key struct{ pkg, test string }
	final := map[key]string{}
	noTestFiles := map[string]bool{}
	events := 0

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // test output lines can be long
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		events++
		pkg := ev.Package
		if pkg == "" {
			pkg = ev.ImportPath
		}
		switch ev.Action {
		case "pass", "fail", "skip":
			final[key{pkg, ev.Test}] = ev.Action
		case "build-fail":
			final[key{pkg, ""}] = "fail"
		case "output":
			if ev.Test == "" && strings.Contains(ev.Output, "[no test files]") {
				noTestFiles[pkg] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Summary{}, err
	}
	if events == 0 {
		return Summary{}, fmt.Errorf("no test events in the input")
	}

	var s Summary
	pkgHasFailedTest := map[string]bool{}
	for k, result := range final {
		if k.test == "" {
			continue
		}
		switch result {
		case "pass":
			s.TestsPassed++
		case "skip":
			s.TestsSkipped++
		case "fail":
			s.TestsFailed++
			pkgHasFailedTest[k.pkg] = true
			s.Failed = append(s.Failed, k.pkg+" "+k.test)
		}
	}
	for k, result := range final {
		if k.test != "" {
			continue
		}
		s.Packages++
		switch {
		case result == "fail":
			s.PackagesFailed++
			if !pkgHasFailedTest[k.pkg] {
				s.Failed = append(s.Failed, k.pkg)
			}
		case noTestFiles[k.pkg]:
			s.PackagesNoTest++
		}
	}
	sort.Strings(s.Failed)
	return s, nil
}
