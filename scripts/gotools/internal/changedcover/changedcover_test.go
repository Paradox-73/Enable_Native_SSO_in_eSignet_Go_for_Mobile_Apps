// SPDX-License-Identifier: MIT
// Copyright (c) 2026 Team Badmosip

package changedcover

import (
	"reflect"
	"strings"
	"testing"
)

const profile = `mode: set
example.com/mod/pkg/a.go:10.20,12.3 2 1
example.com/mod/pkg/a.go:14.2,16.3 3 0
example.com/mod/pkg/a.go:20.2,30.3 4 1
example.com/mod/pkg/b.go:5.1,6.2 1 0
example.com/mod/pkg/b.go:5.1,6.2 1 1
`

const diff = `diff --git a/pkg/a.go b/pkg/a.go
index 1111111..2222222 100644
--- a/pkg/a.go
+++ b/pkg/a.go
@@ -11 +11 @@ func A() {
-	old()
+	changed()
@@ -15,0 +15,2 @@ func A() {
+	added()
+	added()
@@ -40,3 +41,0 @@ func A() {
-	deleted()
diff --git a/pkg/gone.go b/pkg/gone.go
deleted file mode 100644
--- a/pkg/gone.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package pkg
`

func TestParseProfileMergesRepeatedBlocks(t *testing.T) {
	blocks, err := ParseProfile(strings.NewReader(profile), "example.com/mod")
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}
	if len(blocks) != 4 {
		t.Fatalf("got %d blocks, want 4 (the repeated b.go block merged)", len(blocks))
	}
	want := Block{File: "pkg/b.go", StartLine: 5, EndLine: 6, Statements: 1, Covered: true}
	if blocks[3] != want {
		t.Errorf("merged block = %+v, want %+v", blocks[3], want)
	}
}

func TestParseProfileRejectsMalformedLine(t *testing.T) {
	if _, err := ParseProfile(strings.NewReader("mode: set\nnot a profile line\n"), "m"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseDiffCollectsNewSideLines(t *testing.T) {
	set, err := ParseDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}
	want := map[int]bool{11: true, 15: true, 16: true}
	if !reflect.DeepEqual(set.Lines["pkg/a.go"], want) {
		t.Errorf("changed lines = %v, want %v", set.Lines["pkg/a.go"], want)
	}
	if got := set.Files(); !reflect.DeepEqual(got, []string{"pkg/a.go"}) {
		t.Errorf("Files() = %v; a deleted file has no lines to cover", got)
	}
}

func TestParseDiffRejectsMalformedHunk(t *testing.T) {
	_, err := ParseDiff(strings.NewReader("+++ b/x.go\n@@ nonsense @@\n"))
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestComputeCountsOnlyBlocksOnChangedLines(t *testing.T) {
	blocks, _ := ParseProfile(strings.NewReader(profile), "example.com/mod")
	set, _ := ParseDiff(strings.NewReader(diff))
	set.AllLines["pkg/b.go"] = true // an untracked new file counts as changed everywhere
	set.AllLines["pkg/untested.go"] = true

	got := Compute(blocks, set, set.Files())
	want := []FileResult{
		// Line 11 hits the covered block 10-12 (2 statements); lines 15-16 hit the uncovered
		// block 14-16 (3 statements); block 20-30 is untouched upstream code and does not count.
		{File: "pkg/a.go", Statements: 5, Covered: 2, InProfile: true},
		{File: "pkg/b.go", Statements: 1, Covered: 1, InProfile: true},
		{File: "pkg/untested.go", InProfile: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Compute = %+v\nwant %+v", got, want)
	}

	total := Total(got)
	if total.Statements != 6 || total.Covered != 3 || total.Percent() != 50 {
		t.Errorf("Total = %+v (%.1f%%), want 3/6 = 50%%", total, total.Percent())
	}
}

func TestPercentWithoutStatementsIsComplete(t *testing.T) {
	if p := (FileResult{}).Percent(); p != 100 {
		t.Errorf("Percent() = %v, want 100", p)
	}
}

func TestIsGenerated(t *testing.T) {
	cases := map[string]bool{
		"// Code generated from mocks.yaml; DO NOT EDIT.\n\npackage mocks\n":             true,
		"// Copyright x\n\n// Code generated from query.sql. DO NOT EDIT.\npackage db\n": true,
		"package p\n\n// Code generated from nothing. DO NOT EDIT.\n":                    false,
		"// SPDX-License-Identifier: MIT\n\npackage p\n":                                 false,
	}
	for src, want := range cases {
		if got := IsGenerated([]byte(src)); got != want {
			t.Errorf("IsGenerated(%q) = %v, want %v", src, got, want)
		}
	}
}
