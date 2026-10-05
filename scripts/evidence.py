# SPDX-License-Identifier: MIT
# Copyright (c) 2026 Team Badmosip
#
# Helper of scripts/evidence.sh: reads and writes docs/test-evidence.tsv, checks test names and writes
# docs/test-report.md. Not meant to be run by hand; see scripts/evidence.sh.
import sys, os, re, json, glob, datetime
import xml.etree.ElementTree as ET

RECORD, REPORT = os.environ["EV_RECORD"], os.environ["EV_REPORT"]
TODAY, COMMIT = os.environ["EV_TODAY"], os.environ["EV_COMMIT"]
COLS = ["section", "id", "task", "unit", "e2e", "android", "result", "date", "note"]

def load():
    rows = []
    with open(RECORD, encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            p = line.split("\t")
            p += [""] * (len(COLS) - len(p))
            rows.append(dict(zip(COLS, p[:len(COLS)])))
    return rows

def save(rows):
    with open(RECORD, "w", encoding="utf-8", newline="\n") as f:
        f.write("# Test evidence record. One row per result; edited by scripts/evidence.sh --run, and by hand for results\n")
        f.write("# that only a person can produce. Columns: " + "\t".join(COLS) + "\n")
        for r in rows:
            f.write("\t".join(r[c].replace("\t", " ").replace("\n", " ") for c in COLS) + "\n")

def find(rows, rid):
    for r in rows:
        if r["id"] == rid and r["section"] != "limit":
            return r
    sys.exit("evidence: no row " + rid)

def names(cell):
    return [x.strip() for x in cell.split(";") if x.strip()]

def cmd_set(args):
    rid, result, note = args[0], args[1], args[2]
    task = args[3] if len(args) > 3 else None
    rows = load()
    r = find(rows, rid)
    r["result"], r["date"], r["note"] = result, TODAY, note
    if task: r["task"] = task
    save(rows)

def cmd_env(args):
    rows = load()
    for r in rows:
        if r["section"] == "env" and r["id"] == args[0]:
            r["note"] = args[1]; break
    else:
        rows.append(dict(zip(COLS, ["env", args[0], "", "", "", "", "", TODAY, args[1]])))
    save(rows)

def passed_go(files):
    ok = set()
    for fn in files:
        with open(fn, encoding="utf-8", errors="replace") as f:
            for line in f:
                if '"Action":"pass"' in line and '"Test":"' in line:
                    try: ok.add(json.loads(line)["Test"])
                    except Exception: pass
    return ok

def passed_e2e(files):
    ok, bad = set(), set()
    for fn in files:
        with open(fn, encoding="utf-8", errors="replace") as f:
            for line in f:
                m = re.match(r"^--- (PASS|FAIL): (\S+)", line)
                if m: (ok if m.group(1) == "PASS" else bad).add(m.group(2))
    return ok - bad

def junit(dirs):
    cls = {}
    for d in dirs:
        for fn in glob.glob(os.path.join(d, "**", "TEST-*.xml"), recursive=True):
            root = ET.parse(fn).getroot()
            for tc in root.iter("testcase"):
                simple = tc.get("classname", "").split(".")[-1].split("$")[0]
                bad = any(c.tag in ("failure", "error") for c in tc)
                skipped = any(c.tag == "skipped" for c in tc)
                s = cls.setdefault(simple, [0, 0, 0])
                s[0] += 1; s[1] += 1 if bad else 0; s[2] += 1 if skipped else 0
    return cls

def cmd_junit(args):
    # junit <ID>=<dir> ... : sets the JVM total rows, and keeps the class table for the rule rows
    rows = load(); allcls = {}
    for a in args:
        rid, d = a.split("=", 1)
        c = junit([d]); allcls.update(c)
        n = sum(v[0] for v in c.values()); f = sum(v[1] for v in c.values()); s = sum(v[2] for v in c.values())
        r = find(rows, rid)
        r["result"] = "PASS" if f == 0 and n > 0 else "FAIL"
        r["date"] = TODAY
        r["note"] = "%d tests, %d failed, %d skipped, %d test classes" % (n, f, s, len(c))
    save(rows)
    json.dump(allcls, open(os.environ["EV_JUNIT_OUT"], "w"))

def cmd_rules(args):
    # rules --go f1,f2 --e2e f1,f2 --classes file
    opt = dict(zip(args[0::2], args[1::2]))
    go = passed_go([x for x in opt.get("--go", "").split(",") if x])
    e2e = passed_e2e([x for x in opt.get("--e2e", "").split(",") if x])
    cls = json.load(open(opt["--classes"])) if opt.get("--classes") and os.path.exists(opt["--classes"]) else {}
    have_go, have_e2e, have_cls = bool(opt.get("--go")), bool(opt.get("--e2e")), bool(cls)
    rows = load(); changed = 0
    for r in rows:
        if r["section"] != "matrix": continue
        verdict = []   # True/False per piece of evidence available in this run
        unit = names(r["unit"])
        if unit and have_go and r["unit"] != "-":
            verdict.append(all(u in go for u in unit))
        e2 = [x for x in names(r["e2e"]) if x != "unit only" and x != "-"]
        if e2 and have_e2e:
            verdict.append(all(x in e2e for x in e2))
        an = [x for x in names(r["android"]) if x != "-"]
        if an and not any("(device)" in x for x in an) and have_cls:
            verdict.append(all(x in cls and cls[x][1] == 0 and cls[x][0] > 0 for x in an))
        if not verdict: continue
        if r["result"] == "NOT RUN" and "(device)" in r["android"]: continue
        res = "PASS" if all(verdict) else "FAIL"
        if r["result"] != res or r["date"] != TODAY: changed += 1
        r["result"], r["date"] = res, TODAY
    save(rows)
    print("rules updated: %d rows" % changed)

def counts(rows):
    c = {"PASS": 0, "NOT RUN": 0, "FAIL": 0}
    for r in rows:
        if r["section"] in ("agg", "matrix", "manual") and r["result"] in c: c[r["result"]] += 1
    return c

def cmd_check(_):
    rows = load(); limits = {r["id"] for r in rows if r["section"] == "limit"}
    bad = []
    for r in rows:
        if r["section"] not in ("agg", "matrix", "manual"): continue
        if r["result"] == "PASS": continue
        if r["result"] == "NOT RUN":
            m = re.search(r"limit:(L\d+)", r["note"])
            if not m or m.group(1) not in limits: bad.append("%s NOT RUN without a known limit" % r["id"])
        elif r["result"] == "FAIL": bad.append("%s FAIL" % r["id"])
        else: bad.append("%s has result '%s'" % (r["id"], r["result"]))
    c = counts(rows)
    if bad:
        print("evidence: check FAILED: " + "; ".join(bad)); sys.exit(1)
    print("evidence: check OK (%d PASS, %d NOT RUN with a listed limit, 0 FAIL)" % (c["PASS"], c["NOT RUN"]))

def esc(s): return s.replace("|", "\\|")
def code(s):
    return ", ".join("`%s`" % esc(x) for x in names(s)) if s and s != "-" else "-"

def cmd_report(_):
    rows = load(); c = counts(rows)
    env = [r for r in rows if r["section"] == "env"]
    limits = [r for r in rows if r["section"] == "limit"]
    agg = [r for r in rows if r["section"] == "agg"]
    mat = [r for r in rows if r["section"] == "matrix"]
    man = [r for r in rows if r["section"] == "manual"]
    out = []
    w = out.append
    w("# Test report\n")
    w("Written by `bash scripts/evidence.sh` from [test-evidence.tsv](test-evidence.tsv) at commit `%s`; every row keeps the date of the run that set it.\n" % COMMIT)
    w("Do not edit this file by hand: change the record, or run `bash scripts/evidence.sh --run`.\n")
    w("## Summary\n")
    w("- Results in the tables below: %d PASS, %d NOT RUN, %d FAIL." % (c["PASS"], c["NOT RUN"], c["FAIL"]))
    w("- **NOT RUN means the check was not executed**, for the reason listed under [Known limits](#known-limits); it is not a pass.")
    nr = [r["id"] for r in rows if r["section"] in ("agg", "matrix", "manual") and r["result"] == "NOT RUN"]
    w("- Not run: " + (", ".join("`%s`" % i for i in nr) if nr else "nothing") + ".")
    w("- Emulator rows (UI-*, DEMO-FULL and the `b` rows of the rule matrix) are set by hand from a dated emulator run; `--run` does not run the emulator.\n")
    w("## Environment\n")
    w("| Item | Value |\n|---|---|")
    for r in env: w("| %s | %s |" % (esc(r["id"]), esc(r["note"])))
    w("")
    w("## How to reproduce\n")
    w("Run from the repository root in Git Bash; details of each command: [../CONTRIBUTING.md](../CONTRIBUTING.md) and [../scripts/README.md](../scripts/README.md).\n")
    w("```bash")
    w("bash scripts/evidence.sh --run             # runs the checks below and rewrites this report")
    w("bash scripts/evidence.sh --check           # exit 0 when every row is PASS or a listed NOT RUN")
    w("bash scripts/go-test.sh thunder/backend ./internal/oauth/... ./pkg/... --changed-cover")
    w("bash scripts/go-test.sh esignet/esignet-service ./... --changed-cover")
    w("bash scripts/with-stack.sh bash e2e/run.sh")
    w("bash scripts/with-stack.sh bash esignet/postman-collection/native-sso/run.sh")
    w("(cd android && bash gradlew --no-daemon :oidc-client:test :secure-store:testDebugUnitTest :superapp:testDebugUnitTest :childapp:testDebugUnitTest)")
    w("bash scripts/with-stack.sh bash android/scripts/with-emulator.sh bash android/scripts/run-ui-test.sh all   # emulator")
    w("```\n")
    w("## Results by check\n")
    w("| Id | Check | Command | Result | Date | Detail |\n|---|---|---|---|---|---|")
    for r in agg:
        w("| %s | %s | %s | %s | %s | %s |" % (r["id"], esc(r["task"]), ("`%s`" % esc(r["unit"])) if r["unit"] else "", r["result"], r["date"], esc(r["note"])))
    w("")
    w("## Rule matrix\n")
    w("Rules R01-R26 are the server rules and A01-A12 the app rules of [design.md section 20](design.md#20-rules-and-tests). "
      "A row is PASS only when every test named in its Unit, e2e and Android columns passed in the run recorded for it. "
      "A rule that has both a JVM part and a device part has two rows (`a`, `b`); the device row is set from an emulator run.\n")
    w("| Rule | Task | Unit test | e2e test | Android test | Result | Date | Note |\n|---|---|---|---|---|---|---|---|")
    for r in mat:
        w("| %s | %s | %s | %s | %s | %s | %s | %s |" % (r["id"], esc(r["task"]), code(r["unit"]), code(r["e2e"]) if r["e2e"] != "unit only" else "unit only", code(r["android"]), r["result"], r["date"], esc(r["note"])))
    w("")
    w("## Coverage and lint\n")
    for r in agg:
        if r["id"].startswith(("COVER", "LINT", "OPENAPI", "REPO", "DOCS")):
            w("- **%s** (%s, %s): %s. %s" % (r["task"], r["result"], r["date"], "`%s`" % r["unit"] if r["unit"] else "", r["note"]))
    w("")
    w("## Manual runs\n")
    w("| Id | What | Who | Result | Date | Note |\n|---|---|---|---|---|---|")
    for r in man:
        w("| %s | %s | %s | %s | %s | %s |" % (r["id"], esc(r["task"]), esc(r["unit"]) or "-", r["result"], r["date"] or "-", esc(r["note"])))
    w("")
    w("## Known limits\n")
    for r in limits: w("- **%s**: %s" % (r["id"], r["note"]))
    w("")
    open(REPORT, "w", encoding="utf-8", newline="\n").write("\n".join(out))
    print("evidence: %d results at %s" % (c["PASS"] + c["NOT RUN"] + c["FAIL"], COMMIT))

cmd = sys.argv[1]
{"set": cmd_set, "env": cmd_env, "junit": cmd_junit, "rules": cmd_rules, "check": cmd_check, "report": cmd_report}[cmd](sys.argv[2:])
