package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The V50 Docker built-ins land once, with parameter schemas the UI/MCP can
// prompt from, and their scripts parse under bash.
func TestV50DockerBuiltinActionsExistAndParse(t *testing.T) {
	database := openTestDB(t)
	actions, err := database.ListActions()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		category string
		params   []string
	}{
		"Update Docker Containers": {"packages", []string{"STANDALONE", "DRY_RUN", "PRUNE"}},
		"Docker Container Status":  {"monitoring", []string{"INCLUDE_STOPPED", "STATS"}},
	}
	bash, bashErr := exec.LookPath("bash")
	seen := map[string]int{}
	for _, a := range actions {
		w, ok := want[a.Name]
		if !ok {
			continue
		}
		seen[a.Name]++
		if !a.Builtin || a.Category != w.category || len(a.Tags) == 0 {
			t.Errorf("%s: builtin=%v category=%q tags=%v", a.Name, a.Builtin, a.Category, a.Tags)
		}
		if len(a.Parameters) != len(w.params) {
			t.Errorf("%s: want %d parameters, got %+v", a.Name, len(w.params), a.Parameters)
		}
		for i, p := range a.Parameters {
			if i < len(w.params) && p.Name != w.params[i] {
				t.Errorf("%s: parameter %d = %q, want %q", a.Name, i, p.Name, w.params[i])
			}
			if p.Type == "select" && len(p.Options) == 0 {
				t.Errorf("%s: select parameter %s has no options", a.Name, p.Name)
			}
			if p.Type == "boolean" && p.Default != "true" && p.Default != "false" {
				t.Errorf("%s: boolean parameter %s defaults to %q", a.Name, p.Name, p.Default)
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
		if seen[name] != 1 {
			t.Errorf("built-in action %q: want exactly one copy, got %d", name, seen[name])
		}
	}
}
