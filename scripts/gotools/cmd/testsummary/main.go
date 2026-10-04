// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Command testsummary prints the failed tests and a one-line summary of a "go test -json" file.
//
//	testsummary -name thunder/backend results.json
//
// Output (stdout): one "FAIL <package> [<test>]" line per failure, then
// "go-test: <name>: <summary>" ending with "<n> failed". Exit status 1 when anything failed.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/badmosip/esignet-native-sso/scripts/gotools/internal/testjson"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("testsummary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "label for the summary line, usually the module directory")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: testsummary -name <label> <go test -json file>")
		return 2
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "testsummary:", err)
		return 2
	}
	defer f.Close()

	s, err := testjson.Summarize(f)
	if err != nil {
		fmt.Fprintf(stderr, "testsummary: %s: %v\n", fs.Arg(0), err)
		fmt.Fprintf(stdout, "go-test: %s: tests did not run, 1 failed\n", *name)
		return 1
	}
	for _, failed := range s.Failed {
		fmt.Fprintln(stdout, "FAIL", failed)
	}
	fmt.Fprintf(stdout, "go-test: %s: %s\n", *name, s.Line())
	if s.Failures() > 0 {
		return 1
	}
	return 0
}
