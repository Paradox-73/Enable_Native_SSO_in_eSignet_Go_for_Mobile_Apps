// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Command changedcover prints, per changed Go file of a module, the test coverage of the
// statements on changed lines only.
//
// Usage:
//
//	changedcover -module <dir> -profile cover.out -diff changes.diff [-untracked files.txt] [-min 80]
//
// The -diff file is "git diff -U0 --relative" output for the module, so its paths are
// module-relative. The -untracked file lists new files not yet known to git, one module-relative
// path per line; every line of them counts as changed. Test files and files with a generated-code
// marker are skipped: they are not measured code. With -min, a total below that percentage makes
// the exit status 1.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/badmosip/esignet-native-sso/scripts/gotools/internal/changedcover"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("changedcover", flag.ContinueOnError)
	fs.SetOutput(stderr)
	moduleDir := fs.String("module", ".", "module root (the folder with go.mod)")
	profilePath := fs.String("profile", "", "cover profile written by go test -coverprofile")
	diffPath := fs.String("diff", "", "git diff -U0 --relative output for the module")
	untrackedPath := fs.String("untracked", "", "file listing untracked module-relative paths")
	minPercent := fs.Float64("min", 0, "fail when total coverage of changed statements is below this percent")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *profilePath == "" || *diffPath == "" {
		fmt.Fprintln(stderr, "changedcover: -profile and -diff are required")
		return 2
	}

	results, err := measure(*moduleDir, *profilePath, *diffPath, *untrackedPath)
	if err != nil {
		fmt.Fprintln(stderr, "changedcover:", err)
		return 2
	}
	if len(results) == 0 {
		fmt.Fprintln(stdout, "changed-cover: no changed Go files")
		return 0
	}
	for _, r := range results {
		switch {
		case !r.InProfile:
			fmt.Fprintf(stdout, "changed-cover: %s: no coverage data (its package was not tested)\n", r.File)
		case r.Statements == 0:
			fmt.Fprintf(stdout, "changed-cover: %s: no statements on changed lines\n", r.File)
		default:
			fmt.Fprintf(stdout, "changed-cover: %s: %d/%d statements (%.1f%%)\n", r.File, r.Covered, r.Statements, r.Percent())
		}
	}
	total := changedcover.Total(results)
	fmt.Fprintf(stdout, "changed-cover: total %d/%d statements on changed lines (%.1f%%) in %d files\n",
		total.Covered, total.Statements, total.Percent(), len(results))
	if total.Percent() < *minPercent {
		fmt.Fprintf(stdout, "changed-cover: below the minimum of %.1f%%\n", *minPercent)
		return 1
	}
	return 0
}

func measure(moduleDir, profilePath, diffPath, untrackedPath string) ([]changedcover.FileResult, error) {
	modulePath, err := readModulePath(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return nil, err
	}

	pf, err := os.Open(profilePath)
	if err != nil {
		return nil, err
	}
	defer pf.Close()
	blocks, err := changedcover.ParseProfile(pf, modulePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", profilePath, err)
	}

	df, err := os.Open(diffPath)
	if err != nil {
		return nil, err
	}
	defer df.Close()
	changed, err := changedcover.ParseDiff(df)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", diffPath, err)
	}

	if untrackedPath != "" {
		paths, err := readLines(untrackedPath)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			changed.AllLines[p] = true
		}
	}

	var files []string
	for _, f := range changed.Files() {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(moduleDir, filepath.FromSlash(f)))
		if errors.Is(err, os.ErrNotExist) {
			continue // deleted in the working tree after the diff was taken
		}
		if err != nil {
			return nil, err
		}
		if changedcover.IsGenerated(src) {
			continue
		}
		files = append(files, f)
	}
	return changedcover.Compute(blocks, changed, files), nil
}

func readModulePath(goMod string) (string, error) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return "", err
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	return "", fmt.Errorf("%s: no module line", goMod)
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, filepath.ToSlash(l))
		}
	}
	return lines, sc.Err()
}
