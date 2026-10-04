#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Checks the design documents: links, statement-task traceability, threat coverage, test ids
# and diagram sources.
#
#   bash scripts/docs-check.sh FILE.md...
#   bash scripts/docs-check.sh --self-test
#
#   bash scripts/docs-check.sh docs/design.md docs/threat-model.md
#
# - links: every relative Markdown link in FILE resolves to a file or folder, and a "#anchor"
#   names a heading of the target Markdown file (GitHub heading ids). Web links (any "scheme:")
#   are not fetched. Links inside code spans and fenced code blocks are ignored.
# - tasks: a table whose first header cell is "Task" lists the statement tasks; a row counts as
#   traced when its first cell is T1..T10 or B1 (a letter suffix such as T4a counts for T4) and
#   its "test" column names at least one rule id (R01, A03, ...). All 11 must be traced.
# - threats: a table whose first header cell is "Threat" lists threats T<n>; each needs a
#   non-empty "mitigation" column and a "test" column with at least one rule id.
# - rule ids: every rule id named in a task or threat row must be defined as the first cell of
#   a row in a table whose first header cell is "Rule", in one of the FILEs.
# - diagrams: a ```mermaid block directly after the line "<!-- source: PATH -->" must equal the
#   file PATH (relative to the Markdown file), so the rendered copy and the source cannot drift.
# Prints one line per problem, then
#   "docs-check: N broken links; tasks traced X/11; threats with mitigation and test Y/Z";
# exit status 1 on any problem.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

REQUIRED_TASKS="T1 T2 T3 T4 T5 T6 T7 T8 T9 T10 B1"

# scan FILE BLOCKDIR: one tab-separated record per fact found in a Markdown file.
#   A slug                      heading anchor (GitHub rules: lower case, punctuation dropped,
#                               spaces to "-", "-1", "-2" for repeated headings)
#   L line target               link target
#   M line source blockfile     mermaid block tagged with a source comment; body written to blockfile
#   TASK line id traced         row of a "Task" table
#   THREAT line id mit test     row of a "Threat" table (mit, test: 1 when present)
#   RULE id                     rule id defined in a "Rule" table
#   REF line id                 rule id named in a task or threat row
# Kept in awk so it runs the same with GNU awk and mawk (CI) and needs no extra tool.
scan() {
  awk -v blockdir="$2" '
    function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }
    function plain(s) { gsub(/[`*]/, "", s); return trim(s) }
    function cells(line, out,    n, i) {
      gsub(/\\\|/, "\001", line)
      sub(/^[ \t]*\|/, "", line); sub(/\|[ \t]*$/, "", line)
      n = split(line, out, "|")
      for (i = 1; i <= n; i++) { gsub(/\001/, "|", out[i]); out[i] = trim(out[i]) }
      return n
    }
    function slug(h,    s) {
      s = h
      sub(/^#+[ \t]*/, "", s); sub(/[ \t]+#+[ \t]*$/, "", s)
      while (match(s, /\[[^]]*\]\([^)]*\)/)) {   # [text](url) renders as text
        t = substr(s, RSTART + 1, RLENGTH - 1); sub(/\].*/, "", t)
        s = substr(s, 1, RSTART - 1) t substr(s, RSTART + RLENGTH)
      }
      s = tolower(trim(s))
      gsub(/[^a-z0-9 _-]/, "", s)
      gsub(/ /, "-", s)
      if (s in seen) { seen[s]++; s = s "-" seen[s] } else { seen[s] = 0 }
      return s
    }
    function ids(cell, line, kind,    rest, found, id) {
      rest = " " cell " "; found = 0
      while (match(rest, /[^A-Za-z0-9][RA][0-9][0-9][^0-9]/)) {
        id = substr(rest, RSTART + 1, 3)
        found = 1
        if (kind != "") print "REF\t" line "\t" id
        rest = substr(rest, RSTART + 4)
      }
      return found
    }
    function empty(cell,    c) { c = tolower(plain(cell)); return c == "" || c == "-" || c == "n/a" || c == "none" }

    BEGIN { fence = ""; table = ""; intable = 0; pending_source = ""; nblocks = 0 }

    {
      line = $0; sub(/\r$/, "", line)

      # Fenced code blocks: nothing inside counts, except the body of a tagged mermaid block.
      if (fence != "") {
        if (index(trim(line), fence) == 1) {
          if (block != "") close(block)
          fence = ""; block = ""
          next
        }
        if (block != "") print line > block
        next
      }
      if (match(trim(line), /^(```+|~~~+)/)) {
        fence = substr(trim(line), 1, RLENGTH); block = ""
        if (pending_source != "" && trim(line) ~ /^(```+|~~~+)[ \t]*mermaid[ \t]*$/) {
          block = blockdir "/block" (++nblocks) ".mmd"
          printf "" > block
          print "M\t" FNR "\t" pending_source "\t" block
        }
        pending_source = ""; intable = 0
        next
      }
      if (match(line, /^[ \t]*<!--[ \t]*source:[ \t]*[^ \t]+[ \t]*-->[ \t]*$/)) {
        pending_source = line
        sub(/^[ \t]*<!--[ \t]*source:[ \t]*/, "", pending_source); sub(/[ \t]*-->[ \t]*$/, "", pending_source)
        next
      }
      if (trim(line) != "") pending_source = ""

      # Headings.
      if (line ~ /^#{1,6}[ \t]/) { print "A\t" slug(line); intable = 0; next }

      # Links, outside code spans.
      text = line
      gsub(/`[^`]*`/, "", text)
      while (match(text, /\]\([^)]*\)/)) {
        target = substr(text, RSTART + 2, RLENGTH - 3)
        sub(/[ \t]+"[^"]*"$/, "", target)
        print "L\t" FNR "\t" trim(target)
        text = substr(text, RSTART + RLENGTH)
      }

      # Tables.
      if (line !~ /^[ \t]*\|/) { intable = 0; next }
      n = cells(line, c)
      if (!intable) {                      # header row
        intable = 1; sep = 1
        first = tolower(plain(c[1])); table = ""
        if (first == "task") table = "task"
        else if (first == "threat") table = "threat"
        else if (first == "rule") table = "rule"
        testcol = 0; mitcol = 0
        for (i = 2; i <= n; i++) {
          h = tolower(plain(c[i]))
          if (!testcol && h ~ /test/) testcol = i
          if (!mitcol && h ~ /mitigation/) mitcol = i
        }
        next
      }
      if (sep) { sep = 0; if (line ~ /^[ \t|:-]+$/) next }
      id = plain(c[1])
      if (table == "rule" && id ~ /^[RA][0-9][0-9]([^0-9]|$)/) print "RULE\t" substr(id, 1, 3)
      if (table == "task" && match(id, /^(T(10|[1-9])|B1)([a-z]|[^0-9a-z]|$)/)) {
        match(id, /^(T(10|[1-9])|B1)/); base = substr(id, 1, RLENGTH)
        traced = (testcol > 0 && ids(c[testcol], FNR, "task")) ? 1 : 0
        print "TASK\t" FNR "\t" base "\t" traced
      }
      if (table == "threat" && match(id, /^T[0-9]+/)) {
        tid = substr(id, 1, RLENGTH)
        mit = (mitcol > 0 && !empty(c[mitcol])) ? 1 : 0
        tst = (testcol > 0 && ids(c[testcol], FNR, "threat")) ? 1 : 0
        print "THREAT\t" FNR "\t" tid "\t" mit "\t" tst
      }
    }
  ' "$1"
}

# ---------------------------------------------------------------------------------------------
check() {
  local tmp problems=0 broken=0
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/docs-check.XXXXXX")
  # shellcheck disable=SC2064 # expand the folder now: it is local to this function
  trap "rm -rf '$tmp'" RETURN
  problem() { echo "docs-check: $*"; problems=$((problems + 1)); }

  local f n=0
  local -A scanned=()
  for f in "$@"; do
    [ -f "$f" ] || { problem "no such file: $f"; continue; }
    n=$((n + 1)); mkdir -p "$tmp/$n"
    scan "$f" "$tmp/$n" > "$tmp/$n.facts"
    scanned[$f]=$n
  done

  # anchors_of FILE: heading ids of a Markdown file, scanned once per file for link targets.
  local -A anchor_file=()
  anchors_of() {
    local key; key=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
    if [ -z "${anchor_file[$key]+x}" ]; then
      anchor_file[$key]=$tmp/anchors-${#anchor_file[@]}
      mkdir -p "$tmp/x"
      scan "$1" "$tmp/x" | awk -F'\t' '$1 == "A" { print $2 }' > "${anchor_file[$key]}"
    fi
    cat "${anchor_file[$key]}"
  }

  local kind a b c d dir path frag target
  local -A task_traced=() threat_seen=() threat_ok=() rules=()
  local -a refs=()
  for f in "${!scanned[@]}"; do
    dir=$(dirname "$f")
    while IFS=$'\t' read -r kind a b c d; do
      case $kind in
        L)
          target=$b
          [[ $target =~ ^[a-zA-Z][a-zA-Z0-9+.-]*: ]] && continue   # web and mail links are not fetched
          [ -n "$target" ] || { problem "broken link: $f:$a: empty target"; broken=$((broken + 1)); continue; }
          path=${target%%#*}; frag=""
          [[ $target == *#* ]] && frag=${target#*#}
          if [ -z "$path" ]; then path=$f; else path=$dir/$path; fi
          if [ ! -e "$path" ]; then
            problem "broken link: $f:$a: $target (no such file)"; broken=$((broken + 1)); continue
          fi
          if [ -n "$frag" ] && [[ $path == *.md ]] && [ -f "$path" ]; then
            if ! anchors_of "$path" | grep -qxF -- "$frag"; then
              problem "broken link: $f:$a: $target (no heading with id \"$frag\")"; broken=$((broken + 1))
            fi
          fi
          ;;
        M)
          if [ ! -f "$dir/$b" ]; then
            problem "diagram: $f:$a: source $b does not exist"
          elif ! diff -q <(tr -d '\r' < "$dir/$b") "$c" >/dev/null; then
            problem "diagram: $f:$a: block differs from $b"
          fi
          ;;
        TASK)
          # Every row of a task (T4a and T4b both count for T4) must name a rule id.
          [ "${task_traced[$b]:-1}" = 1 ] && task_traced[$b]=$c
          [ "$c" = 1 ] || problem "task $b ($f:$a): row names no rule id"
          ;;
        THREAT)
          threat_seen[$b]=1
          if [ "$c" = 1 ] && [ "$d" = 1 ]; then threat_ok[$b]=1; else
            [ "$c" = 1 ] || problem "threat $b ($f:$a): no mitigation"
            [ "$d" = 1 ] || problem "threat $b ($f:$a): no test id"
          fi
          ;;
        RULE) rules[$a]=1 ;;
        REF) refs+=("$f:$a:$b") ;;
      esac
    done < "$tmp/${scanned[$f]}.facts"
  done

  local r id traced=0 total=0
  for r in "${refs[@]+"${refs[@]}"}"; do
    id=${r##*:}
    [ -n "${rules[$id]+x}" ] || problem "test id $id (${r%:*}) is not defined in a Rule table"
  done
  for id in $REQUIRED_TASKS; do
    if [ "${task_traced[$id]:-0}" = 1 ]; then traced=$((traced + 1)); else problem "task $id not traced (needs a Task table row with a rule id)"; fi
  done
  total=${#threat_seen[@]}
  [ "$total" -gt 0 ] || problem "no Threat table found"

  echo "docs-check: $broken broken links; tasks traced $traced/$(wc -w <<<"$REQUIRED_TASKS" | tr -d ' '); threats with mitigation and test ${#threat_ok[@]}/$total"
  [ $problems -eq 0 ]
}

# ---------------------------------------------------------------------------------------------
self_test() {
  local tmp fail=0 script=$PS03_ROOT/scripts/docs-check.sh
  tmp=$(mktemp -d "${TMPDIR:-/tmp}/docs-check-test.XXXXXX")
  # shellcheck disable=SC2064 # expand the folder now: it is local to this function
  trap "rm -rf '$tmp'" RETURN

  local base=$tmp/base
  mkdir -p "$base/docs/diagrams"
  printf 'sequenceDiagram\n  A->>B: hello\n' > "$base/docs/diagrams/s.mmd"
  printf '# Other\n\n## Part two\n' > "$base/docs/other.md"
  {
    printf '# Design\n\n## 1. Flows (`main`)\n\nSee [other](other.md#part-two), [here](#1-flows-main), [web](https://example.org/x).\n\n'
    printf '<!-- source: diagrams/s.mmd -->\n```mermaid\nsequenceDiagram\n  A->>B: hello\n```\n\n'
    printf '```text\n[not a link](missing.md)\n```\n\nInline `[code](missing.md)` is not a link.\n\n'
    printf '| Task | Requirement | Rules and tests |\n|---|---|---|\n'
    for t in T1 T2 T3 T4a T4b T5 T6 T7 T8 T9 T10 B1; do printf '| %s | x | R01 |\n' "$t"; done
    printf '\n| Rule | Expected |\n|---|---|\n| R01 | ok |\n| A01 | ok |\n'
  } > "$base/docs/design.md"
  printf '# Threats\n\n| Threat | What | Mitigations | Tests |\n|---|---|---|---|\n| T1 x | a | b | R01, A01 |\n| T2 y | a | b | `R01` |\n' \
    > "$base/docs/threat-model.md"

  # expect <case> <exit 0|1> <output fragment> <setup command...>: copies the base documents,
  # runs the setup inside the copy, then the check.
  expect() {
    local name=$1 want=$2 fragment=$3 dir out rc; shift 3
    dir=$tmp/case-$RANDOM; cp -r "$base" "$dir"
    (cd "$dir" && "$@") || { echo "self-test case '$name': setup failed" >&2; fail=1; return; }
    rc=0; out=$(cd "$dir" && bash "$script" docs/design.md docs/threat-model.md 2>&1) || rc=$?
    if [ "$rc" != "$want" ] || ! grep -qF -- "$fragment" <<<"$out"; then
      echo "self-test case '$name': want exit $want with \"$fragment\", got exit $rc:" >&2; echo "$out" >&2; fail=1
    fi
  }
  nothing() { :; }
  edit() { local file=$1; shift; sed -i "$@" "$file"; }
  broken_file() { printf 'Read [gone](gone.md).\n' >> docs/design.md; }
  broken_anchor() { printf 'Read [x](other.md#part-three).\n' >> docs/design.md; }
  repeated_heading() { printf '\n## Part two\n' >> docs/other.md; printf 'Read [x](other.md#part-two-1).\n' >> docs/design.md; }
  missing_task() { edit docs/design.md '/^| T7 |/d'; }
  task_without_test() { edit docs/design.md 's/^| T9 | x | R01 |/| T9 | x | planned |/'; }
  task_part_without_test() { edit docs/design.md 's/^| T4b | x | R01 |/| T4b | x | later |/'; }
  threat_without_test() { printf '| T3 z | a | b | - |\n' >> docs/threat-model.md; }
  threat_without_mitigation() { printf '| T3 z | a | - | R01 |\n' >> docs/threat-model.md; }
  undefined_rule() { edit docs/threat-model.md 's/R01, A01/R01, A77/'; }
  diagram_drift() { printf '  B->>A: bye\n' >> docs/diagrams/s.mmd; }
  crlf_source() { sed -i 's/$/\r/' docs/diagrams/s.mmd; }

  expect "clean documents" 0 "docs-check: 0 broken links; tasks traced 11/11; threats with mitigation and test 2/2" nothing
  expect "missing file" 1 "broken link: docs/design.md:38: gone.md (no such file)" broken_file
  expect "missing anchor" 1 '(no heading with id "part-three")' broken_anchor
  expect "repeated heading gets -1" 0 "docs-check: 0 broken links" repeated_heading
  expect "task row missing" 1 "tasks traced 10/11" missing_task
  expect "task row without rule id" 1 "task T9 not traced" task_without_test
  expect "one of two rows of a task without rule id" 1 "tasks traced 10/11" task_part_without_test
  expect "threat without test id" 1 "threat T3 (docs/threat-model.md:7): no test id" threat_without_test
  expect "threat without mitigation" 1 "threats with mitigation and test 2/3" threat_without_mitigation
  expect "undefined rule id" 1 "test id A77" undefined_rule
  expect "diagram differs from its source" 1 "block differs from diagrams/s.mmd" diagram_drift
  expect "CRLF diagram source still matches" 0 "docs-check: 0 broken links" crlf_source

  if [ $fail -eq 0 ]; then echo "docs-check: self-test OK"; else echo "docs-check: self-test FAILED"; return 1; fi
}

# ---------------------------------------------------------------------------------------------
case ${1:-} in
  ''|-h|--help) ps03_usage ;;
  --self-test) self_test ;;
  *) check "$@" ;;
esac
