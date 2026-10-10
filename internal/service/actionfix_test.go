package service

import (
	"strings"
	"testing"
)

func TestAutoFixAppliesMechanicalFindingsOnly(t *testing.T) {
	script := "#!/bin/bash\napt-get update\nsudo apt-get install nginx\ndnf install httpd\nrm -rf \"$DIR\"/*\n"
	findings, _ := LintAction(script, nil)
	fixed, changes, remaining := AutoFix(script, findings)
	want := "#!/bin/bash\nset -euo pipefail\nexport DEBIAN_FRONTEND=noninteractive\napt-get update\napt-get install -y nginx\ndnf install -y httpd\nrm -rf \"$DIR\"/*\n"
	if fixed != want {
		t.Errorf("fixed script:\n%s\nwant:\n%s", fixed, want)
	}
	applied := 0
	for _, c := range changes {
		if c.Applied {
			applied++
		}
		if c.By != "auto" {
			t.Errorf("auto fix must be marked auto: %+v", c)
		}
	}
	if applied != 5 { // set -e, DEBIAN_FRONTEND, apt -y, dnf -y, sudo removed
		t.Errorf("applied %d changes: %+v", applied, changes)
	}
	rules := map[string]bool{}
	for _, f := range remaining {
		rules[f.Rule] = true
	}
	if !rules["rm-rf-unguarded"] || !rules["parameter-undeclared"] || rules["apt-interactive"] {
		t.Errorf("remaining: %v", rules)
	}
	// The fixed script lints clean of the mechanical rules.
	after, _ := LintAction(fixed, nil)
	for _, f := range after {
		if autoFixable[f.Rule] {
			t.Errorf("still flagged after fix: %s", f.Rule)
		}
	}
	// Idempotent: nothing to do twice.
	again, changes2, _ := AutoFix(fixed, findings)
	if again != fixed {
		t.Error("second pass changed the script")
	}
	for _, c := range changes2 {
		if c.Applied && c.Rule != "apt-interactive" && c.Rule != "dnf-interactive" && c.Rule != "sudo-inside" {
			t.Errorf("second pass re-applied %s", c.Rule)
		}
	}
	_ = strings.TrimSpace
}

func TestLintMarksFixability(t *testing.T) {
	findings, _ := LintAction("apt-get install nginx\ncurl x | sh\nchmod 777 /x\n", nil)
	got := map[string]string{}
	for _, f := range findings {
		got[f.Rule] = f.Fix
	}
	if got["missing-set-e"] != "auto" || got["apt-interactive"] != "auto" || got["pipe-to-shell"] != "ai" || got["chmod-777"] != "ai" {
		t.Errorf("fixability: %v", got)
	}
}
