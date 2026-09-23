package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSkillSync puts a stub `ws-skills-sync` first on PATH for one test.
//
// A stub rather than the real tool: this file is about how OMCA *reacts* to
// each answer, and the real tool needs a dev_env checkout that CI does not
// have. What the stub must not do is invent an interface -- the exit codes
// below are the ones ws-skills-sync documents (0 clean, 1 drift, 2 not
// applicable), and `test_the_exit_codes_this_check_depends_on` upstream is
// what keeps the two from parting ways.
func fakeSkillSync(t *testing.T, exitCode int, output string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n"
	if output != "" {
		script += "printf '%s\\n' " + singleQuote(output) + "\n"
	}
	script += "exit " + itoa(exitCode) + "\n"
	path := filepath.Join(dir, "ws-skills-sync")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func singleQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func repoWithSkills(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSkillDelivery_CleanRepoIsOK(t *testing.T) {
	root := repoWithSkills(t)
	fakeSkillSync(t, 0, "")
	f := checkSkillDelivery(root)
	if f.Status != statusOK {
		t.Fatalf("want OK, got %s: %s", f.Status, f.Detail)
	}
}

// The check exists for this case: hashes green is not the same as delivered,
// and a drifted copy keeps loading while saying something the source does not.
func TestSkillDelivery_DriftIsFailNotWarn(t *testing.T) {
	root := repoWithSkills(t)
	fakeSkillSync(t, 1, "  update audit  (skills/audit/SKILL.md)")
	f := checkSkillDelivery(root)
	if f.Status != statusFail {
		t.Fatalf("drift must fail the doctor, got %s: %s", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "audit") {
		t.Fatalf("the upstream tool's own words must survive: %q", f.Detail)
	}
}

// Absence must not read as health. A worktree that vendors skills and cannot
// be checked is unknown, and unknown is a WARN that says so in words.
func TestSkillDelivery_MissingToolIsWarnNotOK(t *testing.T) {
	root := repoWithSkills(t)
	t.Setenv("PATH", t.TempDir())
	f := checkSkillDelivery(root)
	if f.Status != statusWarn {
		t.Fatalf("want WARN when the tool is absent, got %s: %s", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "not the same as fine") {
		t.Fatalf("the detail must say unknown, not imply clean: %q", f.Detail)
	}
}

// ... but a repository that vendors nothing genuinely has nothing to check,
// and warning there would train people to ignore this line.
func TestSkillDelivery_RepoWithoutVendoredSkillsIsOK(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	f := checkSkillDelivery(root)
	if f.Status != statusOK {
		t.Fatalf("want OK for a repo that vendors nothing, got %s: %s", f.Status, f.Detail)
	}
}

func TestSkillDelivery_NotApplicableExitCodeIsWarn(t *testing.T) {
	root := repoWithSkills(t)
	fakeSkillSync(t, 2, "ws-skills-sync: /x is not a git checkout")
	f := checkSkillDelivery(root)
	if f.Status != statusWarn {
		t.Fatalf("want WARN for the not-applicable exit code, got %s", f.Status)
	}
}

// A drifting repository must not be able to push every other doctor line off
// the screen, and must not lose the first lines either.
func TestSkillDelivery_LongOutputIsBoundedAndKeepsTheHead(t *testing.T) {
	root := repoWithSkills(t)
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "update skill"+itoa(i))
	}
	fakeSkillSync(t, 1, strings.Join(lines, "\n"))
	f := checkSkillDelivery(root)
	if strings.Count(f.Detail, ";") > 8 {
		t.Fatalf("detail is unbounded: %q", f.Detail)
	}
	if !strings.Contains(f.Detail, "skill0") {
		t.Fatalf("the first line must survive truncation: %q", f.Detail)
	}
	if !strings.Contains(f.Detail, "more line") {
		t.Fatalf("truncation must say it truncated: %q", f.Detail)
	}
}

// A check that cannot fail is not verifying anything (doctor.go's own bar).
func TestSkillDelivery_CanReportFail(t *testing.T) {
	root := repoWithSkills(t)
	fakeSkillSync(t, 1, "drift")
	if checkSkillDelivery(root).Status != statusFail {
		t.Fatal("this check must be able to report FAIL")
	}
}
