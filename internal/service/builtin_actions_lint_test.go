package service

import "testing"

// The Docker built-ins (V50) must pass Forgemill's own script check cleanly:
// no medium or high finding, and no parameter they declare but never read.
// Users are told to fix those in their own scripts; the ones we ship can't
// be worse. (Older built-ins pre-date the checker and still trip it; they
// are not covered here.)
func TestDockerBuiltinActionsPassTheScriptCheck(t *testing.T) {
	d := newTestDB(t)
	actions, err := d.ListActions()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"Update Docker Containers": false, "Docker Container Status": false}
	for _, a := range actions {
		if _, ok := want[a.Name]; !ok || !a.Builtin {
			continue
		}
		want[a.Name] = true
		findings, distro := LintAction(a.Script, a.Parameters)
		for _, f := range findings {
			if f.Severity == "high" || f.Severity == "medium" || f.Rule == "parameter-unused" {
				t.Errorf("%s: %s [%s] line %d: %s — %s", a.Name, f.Severity, f.Rule, f.Line, f.Title, f.Detail)
			}
		}
		if !distro.Debian || !distro.RHEL {
			t.Errorf("%s: should be distro-neutral, got %+v", a.Name, distro)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("built-in %q not found", name)
		}
	}
}
