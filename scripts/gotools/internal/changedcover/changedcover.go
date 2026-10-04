// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

// Package changedcover measures test coverage of the Go statements on changed lines only.
//
// A file the team touches inside an imported upstream module keeps upstream's own untested
// branches; whole-file coverage would mix those with the team's lines. Here a cover-profile block
// (a run of statements without branches, as written by "go test -coverprofile") counts when at
// least one of its lines was added or changed according to "git diff -U0". The block granularity
// is the finest the profile offers: it has no per-statement line numbers.
package changedcover

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Block is one cover-profile entry, with the file given relative to the module root.
type Block struct {
	File               string
	StartLine, EndLine int
	Statements         int
	Covered            bool
}

// ParseProfile reads a cover profile ("mode: ..." then "path/file.go:l.c,l.c stmts count" lines).
// modulePath (from go.mod) is stripped from file names so they match module-relative diff paths.
// Blocks listed more than once (one per test binary when several packages are tested) are merged:
// a block is covered when any listing has a non-zero count.
func ParseProfile(r io.Reader, modulePath string) ([]Block, error) {
	type pos struct {
		file   string
		sl, sc int
		el, ec int
	}
	merged := map[pos]*Block{}
	var order []pos

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// The file name may contain ':' only on Windows drive letters, which profiles written in
		// the Linux container never have; the last ':' separates name from position.
		colon := strings.LastIndexByte(line, ':')
		if colon < 0 {
			return nil, fmt.Errorf("profile line %d: no position: %q", lineNo, line)
		}
		var p pos
		var stmts, count int
		if _, err := fmt.Sscanf(line[colon+1:], "%d.%d,%d.%d %d %d", &p.sl, &p.sc, &p.el, &p.ec, &stmts, &count); err != nil {
			return nil, fmt.Errorf("profile line %d: %w: %q", lineNo, err, line)
		}
		p.file = strings.TrimPrefix(line[:colon], modulePath+"/")
		if b, ok := merged[p]; ok {
			b.Covered = b.Covered || count > 0
			continue
		}
		merged[p] = &Block{File: p.file, StartLine: p.sl, EndLine: p.el, Statements: stmts, Covered: count > 0}
		order = append(order, p)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	blocks := make([]Block, 0, len(order))
	for _, p := range order {
		blocks = append(blocks, *merged[p])
	}
	return blocks, nil
}

// LineSet holds changed line numbers per file. AllLines marks a file that is new as a whole.
type LineSet struct {
	Lines    map[string]map[int]bool
	AllLines map[string]bool
}

// NewLineSet returns an empty LineSet.
func NewLineSet() LineSet {
	return LineSet{Lines: map[string]map[int]bool{}, AllLines: map[string]bool{}}
}

// Files returns every file with at least one changed line, sorted.
func (s LineSet) Files() []string {
	seen := map[string]bool{}
	for f, lines := range s.Lines {
		if len(lines) > 0 {
			seen[f] = true
		}
	}
	for f := range s.AllLines {
		seen[f] = true
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

func (s LineSet) changed(file string, from, to int) bool {
	if s.AllLines[file] {
		return true
	}
	lines := s.Lines[file]
	for l := from; l <= to; l++ {
		if lines[l] {
			return true
		}
	}
	return false
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// ParseDiff collects the added and changed lines (new-file side) of a "git diff -U0" stream.
// Deleted files and pure deletions contribute no lines: there is nothing left to cover.
func ParseDiff(r io.Reader) (LineSet, error) {
	set := NewLineSet()
	current := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			name := strings.TrimPrefix(line, "+++ ")
			if name == "/dev/null" {
				current = ""
				continue
			}
			current = strings.TrimPrefix(name, "b/")
		case strings.HasPrefix(line, "@@ "):
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				return set, fmt.Errorf("malformed hunk header %q", line)
			}
			if current == "" {
				continue
			}
			start, _ := strconv.Atoi(m[1])
			count := 1
			if m[2] != "" {
				count, _ = strconv.Atoi(m[2])
			}
			if set.Lines[current] == nil {
				set.Lines[current] = map[int]bool{}
			}
			for l := start; l < start+count; l++ {
				set.Lines[current][l] = true
			}
		}
	}
	return set, sc.Err()
}

// FileResult is the coverage of the statements on changed lines of one file.
type FileResult struct {
	File       string
	Statements int
	Covered    int
	InProfile  bool // false when no package containing the file ran in the tests
}

// Percent returns the covered share in percent, 100 when there is nothing to cover.
func (r FileResult) Percent() float64 {
	if r.Statements == 0 {
		return 100
	}
	return 100 * float64(r.Covered) / float64(r.Statements)
}

// Compute intersects profile blocks with changed lines for every file in files.
func Compute(blocks []Block, changed LineSet, files []string) []FileResult {
	byFile := map[string][]Block{}
	for _, b := range blocks {
		byFile[b.File] = append(byFile[b.File], b)
	}
	results := make([]FileResult, 0, len(files))
	for _, f := range files {
		r := FileResult{File: f}
		fileBlocks, ok := byFile[f]
		r.InProfile = ok
		for _, b := range fileBlocks {
			if !changed.changed(f, b.StartLine, b.EndLine) {
				continue
			}
			r.Statements += b.Statements
			if b.Covered {
				r.Covered += b.Statements
			}
		}
		results = append(results, r)
	}
	return results
}

// Total adds up the results of files that are in the profile.
func Total(results []FileResult) FileResult {
	t := FileResult{File: "total", InProfile: true}
	for _, r := range results {
		if !r.InProfile {
			continue
		}
		t.Statements += r.Statements
		t.Covered += r.Covered
	}
	return t
}

var generatedMarker = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// IsGenerated reports whether Go source carries the standard generated-code marker
// (https://go.dev/s/generatedcode) before its package clause.
func IsGenerated(src []byte) bool {
	head := src
	if i := indexPackageClause(src); i >= 0 {
		head = src[:i]
	}
	return generatedMarker.Match(head)
}

func indexPackageClause(src []byte) int {
	for i := 0; i < len(src); {
		end := i
		for end < len(src) && src[end] != '\n' {
			end++
		}
		if strings.HasPrefix(string(src[i:end]), "package ") {
			return i
		}
		i = end + 1
	}
	return -1
}
