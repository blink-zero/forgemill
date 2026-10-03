package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The V40 built-in actions must land with parameter schemas the UI/MCP can
// prompt from, and their scripts must at least parse under bash.
func TestV40BuiltinActionsExistWithParametersAndParseableScripts(t *testing.T) {
	database := openTestDB(t)
	actions, err := database.ListActions()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"Format and Mount New Disk":       {"DEVICE", "FILESYSTEM", "MOUNT_POINT", "LABEL"},
		"Configure New Network Interface": {"INTERFACE", "MODE", "ADDRESS", "GATEWAY", "DNS"},
	}
	bash, bashErr := exec.LookPath("bash")
	for _, a := range actions {
		params, ok := want[a.Name]
		if !ok {
			continue
		}
		delete(want, a.Name)
		if !a.Builtin || a.Category != "scripts" || len(a.Tags) == 0 {
			t.Errorf("%s: builtin=%v category=%q tags=%v", a.Name, a.Builtin, a.Category, a.Tags)
		}
		if len(a.Parameters) != len(params) {
			t.Errorf("%s: want %d parameters, got %+v", a.Name, len(params), a.Parameters)
		}
		for i, p := range a.Parameters {
			if i < len(params) && p.Name != params[i] {
				t.Errorf("%s: parameter %d = %q, want %q", a.Name, i, p.Name, params[i])
			}
			if p.Type == "select" && len(p.Options) == 0 {
				t.Errorf("%s: select parameter %s has no options", a.Name, p.Name)
			}
		}
		if bashErr == nil {
			f := filepath.Join(t.TempDir(), "script.sh")
			if err := os.WriteFile(f, []byte(a.Script), 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(bash, "-n", f).CombinedOutput(); err != nil {
				t.Errorf("%s: script does not parse: %v\n%s", a.Name, err, out)
			}
		}
	}
	for name := range want {
		t.Errorf("built-in action %q was not inserted by V40", name)
	}
}
