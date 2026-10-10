package service

import (
	"regexp"
	"sort"
	"strings"
)

// Deterministic fixes for the mechanical lint findings. Each edit is small
// and local; anything needing judgement is left to the model (Fix "ai") or
// the user. Applied bottom-up so earlier line numbers stay valid, with the
// top-of-file insertions done last.

// FixChange reports what happened to one selected finding.
type FixChange struct {
	Rule    string `json:"rule,omitempty"`
	Title   string `json:"title"`
	Line    int    `json:"line,omitempty"`
	Applied bool   `json:"applied"`
	By      string `json:"by"` // auto | model
	Note    string `json:"note,omitempty"`
}

var (
	reAptCmd   = regexp.MustCompile(`\b(apt-get|apt)\s+(install|upgrade|dist-upgrade|remove|purge)\b`)
	reDnfCmd   = regexp.MustCompile(`\b(dnf|yum)\s+(install|update|upgrade|remove)\b`)
	reLeadSudo = regexp.MustCompile(`^(\s*)sudo\s+`)
	reShebang  = regexp.MustCompile(`^#!`)
)

// AutoFix applies the deterministic fixes for the selected findings and
// returns the new script, what was applied, and the findings still open.
func AutoFix(script string, selected []Finding) (string, []FixChange, []Finding) {
	lines := strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n")
	var changes []FixChange
	var remaining []Finding
	wantSetE, wantFrontend := false, false

	// Line edits, highest line first.
	lineEdits := []Finding{}
	for _, f := range selected {
		switch f.Rule {
		case "missing-set-e":
			wantSetE = true
		case "apt-frontend":
			wantFrontend = true
		case "apt-interactive", "dnf-interactive", "sudo-inside":
			lineEdits = append(lineEdits, f)
		default:
			remaining = append(remaining, f)
		}
	}
	sort.Slice(lineEdits, func(i, j int) bool { return lineEdits[i].Line > lineEdits[j].Line })
	for _, f := range lineEdits {
		idx := f.Line - 1
		if idx < 0 || idx >= len(lines) {
			changes = append(changes, FixChange{Rule: f.Rule, Title: f.Title, Line: f.Line, By: "auto", Note: "line no longer exists"})
			continue
		}
		l := lines[idx]
		switch f.Rule {
		case "apt-interactive":
			if m := reAptCmd.FindStringIndex(l); m != nil && !strings.Contains(l, " -y") && !strings.Contains(l, "--yes") {
				lines[idx] = l[:m[1]] + " -y" + l[m[1]:]
				changes = append(changes, FixChange{Rule: f.Rule, Title: f.Title, Line: f.Line, Applied: true, By: "auto", Note: "added -y"})
				continue
			}
		case "dnf-interactive":
			if m := reDnfCmd.FindStringIndex(l); m != nil && !strings.Contains(l, " -y") && !strings.Contains(l, "--assumeyes") {
				lines[idx] = l[:m[1]] + " -y" + l[m[1]:]
				changes = append(changes, FixChange{Rule: f.Rule, Title: f.Title, Line: f.Line, Applied: true, By: "auto", Note: "added -y"})
				continue
			}
		case "sudo-inside":
			if reLeadSudo.MatchString(l) {
				lines[idx] = reLeadSudo.ReplaceAllString(l, "$1")
				changes = append(changes, FixChange{Rule: f.Rule, Title: f.Title, Line: f.Line, Applied: true, By: "auto", Note: "removed sudo"})
				continue
			}
		}
		changes = append(changes, FixChange{Rule: f.Rule, Title: f.Title, Line: f.Line, By: "auto", Note: "the line changed since the check; re-run Check"})
	}

	// Top-of-file insertions: after the shebang (and after set -e for the
	// DEBIAN_FRONTEND export), skipping blank lines already there.
	insertAt := 0
	if len(lines) > 0 && reShebang.MatchString(lines[0]) {
		insertAt = 1
	}
	if wantSetE {
		hasSetE := false
		for i := 0; i < len(lines) && i < 25; i++ {
			if reSetE.MatchString(lines[i]) {
				hasSetE = true
				break
			}
		}
		if hasSetE {
			changes = append(changes, FixChange{Rule: "missing-set-e", Title: "Script does not stop on the first error", By: "auto", Note: "already present"})
		} else {
			lines = append(lines[:insertAt], append([]string{"set -euo pipefail"}, lines[insertAt:]...)...)
			changes = append(changes, FixChange{Rule: "missing-set-e", Title: "Script does not stop on the first error", Applied: true, By: "auto", Note: "added set -euo pipefail"})
			insertAt++
		}
	}
	if wantFrontend {
		if strings.Contains(strings.Join(lines, "\n"), "DEBIAN_FRONTEND") {
			changes = append(changes, FixChange{Rule: "apt-frontend", Title: "apt without DEBIAN_FRONTEND=noninteractive", By: "auto", Note: "already present"})
		} else {
			// place after an existing set -e line if there is one near the top
			at := insertAt
			for i := 0; i < len(lines) && i < 25; i++ {
				if reSetE.MatchString(lines[i]) {
					at = i + 1
					break
				}
			}
			lines = append(lines[:at], append([]string{"export DEBIAN_FRONTEND=noninteractive"}, lines[at:]...)...)
			changes = append(changes, FixChange{Rule: "apt-frontend", Title: "apt without DEBIAN_FRONTEND=noninteractive", Applied: true, By: "auto", Note: "added export DEBIAN_FRONTEND=noninteractive"})
		}
	}
	return strings.Join(lines, "\n"), changes, remaining
}
