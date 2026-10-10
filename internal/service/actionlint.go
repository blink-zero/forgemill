package service

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/forgemill/forgemill/internal/db/models"
)

// Deterministic checks on an action script. They run with AI off and are
// merged with the model's findings when it is on, so the editor's Check
// panel always has something true to say. Every rule is conservative: it
// points at a line and suggests a fix, it does not block saving.

// Finding is one thing worth knowing about a script.
type Finding struct {
	Severity   string `json:"severity"` // critical | high | medium | low | info
	Source     string `json:"source"`   // lint | model
	Rule       string `json:"rule,omitempty"`
	Line       int    `json:"line,omitempty"` // 1-based; 0 = whole script
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	// Fix says how this finding can be corrected: "auto" (a deterministic
	// edit Forgemill applies itself), "ai" (needs the model), "" (manual).
	Fix string `json:"fix,omitempty"`
}

// autoFixable are the lint rules with a safe mechanical fix.
var autoFixable = map[string]bool{"missing-set-e": true, "apt-interactive": true, "dnf-interactive": true, "apt-frontend": true, "sudo-inside": true}

// aiFixable are the rules the model can usually fix well on its own; the
// rest are judgement calls the user should make (or ask the model about
// with context).
var aiFixable = map[string]bool{"rm-rf-unguarded": true, "pipe-to-shell": true, "chmod-777": true, "secret-literal": true, "hardcoded-address": true, "eval-variable": true, "interactive-command": true, "parameter-undeclared": true, "distro-debian-only": true, "distro-rhel-only": true, "distro-unbranched": true, "reboots-host": true, "disk-destructive": true}

// DistroSupport is what the script appears to assume about the guest.
type DistroSupport struct {
	Debian bool   `json:"debian"`
	RHEL   bool   `json:"rhel"`
	Notes  string `json:"notes,omitempty"`
}

var severityRank = map[string]int{"info": 0, "low": 1, "medium": 2, "high": 3, "critical": 4}

// riskFromFindings is the highest severity present, floored at low.
func riskFromFindings(findings []Finding) string {
	risk := "low"
	for _, f := range findings {
		if severityRank[f.Severity] > severityRank[risk] && f.Severity != "info" {
			risk = f.Severity
		}
	}
	return risk
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if severityRank[findings[i].Severity] != severityRank[findings[j].Severity] {
			return severityRank[findings[i].Severity] > severityRank[findings[j].Severity]
		}
		return findings[i].Line < findings[j].Line
	})
}

var (
	reSetE          = regexp.MustCompile(`^\s*set\s+-[a-zA-Z]*e|^\s*set\s+-o\s+errexit`)
	reRmRfRoot      = regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r|--recursive[^\n]*--force|--force[^\n]*--recursive)\s+(--\s+)?("?'?/\*?"?'?|"?\$\{?[A-Za-z_][A-Za-z0-9_]*\}?"?(/\*|/)?)(\s|$)`)
	reRmRfGuarded   = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*:\?`)
	rePipeToShell   = regexp.MustCompile(`\b(curl|wget)\b[^\n|]*\|\s*(sudo\s+)?(ba|z|da)?sh\b`)
	reDiskDestroy   = regexp.MustCompile(`\b(mkfs(\.[a-z0-9]+)?|wipefs|sgdisk\s+(--zap-all|-Z)|dd\s+if=|parted\b[^\n]*\brm\b|fdisk\s+/dev)`)
	reChmod777      = regexp.MustCompile(`\bchmod\s+(-R\s+)?0?777\b`)
	reIPv4Literal   = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\b`)
	reSecretLiteral = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API_KEY|APIKEY|PRIVATE_KEY)[A-Z0-9_]*)=(["']?)([^"'\s$][^"'\s]*)`)
	reAptInstall    = regexp.MustCompile(`\b(apt-get|apt)\s+(install|upgrade|dist-upgrade|remove|purge|autoremove)\b([^\n]*)`)
	reDnfInstall    = regexp.MustCompile(`\b(dnf|yum)\s+(install|update|upgrade|remove)\b([^\n]*)`)
	reAptAny        = regexp.MustCompile(`\b(apt-get|apt|dpkg)\b`)
	reRhelAny       = regexp.MustCompile(`\b(dnf|yum|rpm)\b`)
	reOSRelease     = regexp.MustCompile(`/etc/os-release|\bID_LIKE\b|\$ID\b|\$\{ID`)
	reReboot        = regexp.MustCompile(`^\s*(sudo\s+)?(reboot|shutdown|init\s+6|systemctl\s+(reboot|poweroff|halt))\b`)
	reEvalVar       = regexp.MustCompile(`\beval\s+[^\n]*\$`)
	reSudo          = regexp.MustCompile(`^\s*sudo\s`)
	reVarRef        = regexp.MustCompile(`\$\{?([A-Z][A-Z0-9_]*)\b`)
	reVarAssign     = regexp.MustCompile(`(?m)^\s*(?:export\s+|local\s+|readonly\s+|declare\s+(?:-[a-zA-Z]+\s+)?)?([A-Z][A-Z0-9_]*)=`)
	reReadVar       = regexp.MustCompile(`\bread\s+(?:-[a-zA-Z]+\s+)*([A-Z][A-Z0-9_]*)`)
	reForVar        = regexp.MustCompile(`\bfor\s+([A-Z][A-Z0-9_]*)\s+in\b`)
	reInteractive   = regexp.MustCompile(`^\s*(passwd|visudo|nano|vim?|less|more|top|htop|dpkg-reconfigure)\b`)
)

// shellEnv are upper-case variables a script may use without declaring.
var shellEnv = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "PWD": true, "OLDPWD": true, "TERM": true, "LANG": true, "LANGUAGE": true, "TZ": true,
	"HOSTNAME": true, "UID": true, "EUID": true, "PPID": true, "RANDOM": true, "SECONDS": true, "LINENO": true, "IFS": true, "OSTYPE": true, "BASH": true, "BASH_VERSION": true,
	"BASH_SOURCE": true, "FUNCNAME": true, "PIPESTATUS": true, "TMPDIR": true, "DEBIAN_FRONTEND": true, "NEEDRESTART_MODE": true, "EDITOR": true, "PAGER": true, "COLUMNS": true, "LINES": true,
	// /etc/os-release fields once sourced
	"ID": true, "ID_LIKE": true, "NAME": true, "PRETTY_NAME": true, "VERSION_ID": true, "VERSION": true, "VERSION_CODENAME": true, "UBUNTU_CODENAME": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "SUDO_USER": true,
}

// LintAction runs every deterministic rule.
func LintAction(script string, params []models.ActionParameter) ([]Finding, DistroSupport) {
	var findings []Finding
	add := func(sev, rule string, line int, title, detail, suggestion string) {
		fix := ""
		if autoFixable[rule] {
			fix = "auto"
		} else if aiFixable[rule] {
			fix = "ai"
		}
		findings = append(findings, Finding{Severity: sev, Source: "lint", Rule: rule, Line: line, Title: title, Detail: detail, Suggestion: suggestion, Fix: fix})
	}
	lines := strings.Split(script, "\n")

	// set -e in the preamble
	hasSetE := false
	for i, l := range lines {
		if i > 25 {
			break
		}
		if reSetE.MatchString(l) {
			hasSetE = true
			break
		}
	}
	if !hasSetE && len(strings.TrimSpace(script)) > 0 {
		add("medium", "missing-set-e", 0, "Script does not stop on the first error",
			"Without `set -e` (or `set -euo pipefail`) a failed step is ignored and the next ones run against a half-changed system; the run still reports success.",
			"Add `set -euo pipefail` after the shebang.")
	}

	usesApt, usesRhel, hasOSRelease, hasNonInteractive := false, false, false, strings.Contains(script, "DEBIAN_FRONTEND")
	declared := map[string]bool{}
	for _, p := range params {
		declared[p.Name] = true
	}
	assigned := map[string]bool{}
	for _, m := range reVarAssign.FindAllStringSubmatch(script, -1) {
		assigned[m[1]] = true
	}
	for _, m := range reReadVar.FindAllStringSubmatch(script, -1) {
		assigned[m[1]] = true
	}
	for _, m := range reForVar.FindAllStringSubmatch(script, -1) {
		assigned[m[1]] = true
	}
	used := map[string]int{}
	reportedIP := false

	for i, raw := range lines {
		n := i + 1
		l := raw
		if idx := strings.Index(l, "#"); idx >= 0 && !strings.Contains(l[:idx], "$") && (idx == 0 || l[idx-1] != '$') {
			// drop trailing comments (keep "#" inside "${#VAR}" and "$#")
			l = l[:idx]
		}
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		for _, m := range reVarRef.FindAllStringSubmatch(l, -1) {
			if _, seen := used[m[1]]; !seen {
				used[m[1]] = n
			}
		}
		if reRmRfRoot.MatchString(l) && !reRmRfGuarded.MatchString(l) {
			sev := "high"
			if strings.Contains(l, " /") || strings.Contains(l, " \"/") || strings.Contains(l, " '/") {
				sev = "critical"
			}
			add(sev, "rm-rf-unguarded", n, "Recursive delete of a root path or an unchecked variable",
				"`rm -rf` runs here as root. If the variable is empty or wrong this removes far more than intended.",
				"Guard the variable with `: \"${VAR:?}\"`, refuse `/`, and prefer deleting a specific, known path.")
		}
		if rePipeToShell.MatchString(l) {
			add("high", "pipe-to-shell", n, "Downloads a script and runs it unseen",
				"`curl … | sh` executes whatever the server returns, as root, with no checksum and no review.",
				"Download to a file, verify a checksum or signature, then run it — or install from the distro's packages.")
		}
		if reDiskDestroy.MatchString(l) {
			add("high", "disk-destructive", n, "Formats, wipes or overwrites a block device",
				"mkfs/wipefs/dd/sgdisk destroy whatever is on the device. The built-in disk action refuses devices that already carry partitions or a filesystem.",
				"Check for existing partitions/filesystems first (`lsblk -no FSTYPE,PARTTYPE`) and refuse if any are present; take the device from a parameter.")
		}
		if reChmod777.MatchString(l) {
			add("medium", "chmod-777", n, "World-writable permissions",
				"777 lets every user on the system modify or replace the file.",
				"Use the narrowest mode that works (e.g. 755 for directories, 644 for files) and chown to the owning user.")
		}
		if m := reSecretLiteral.FindStringSubmatch(l); m != nil && !strings.HasPrefix(m[3], "$") && !strings.HasPrefix(m[3], "«") {
			add("high", "secret-literal", n, "A secret is written into the script",
				fmt.Sprintf("`%s` is assigned a literal value. Scripts are stored, versioned and shown in the UI.", m[1]),
				fmt.Sprintf("Make `%s` a parameter of type password; it is passed as an environment variable at run time and never stored in the script.", m[1]))
		}
		if !reportedIP && reIPv4Literal.MatchString(l) {
			ip := reIPv4Literal.FindString(l)
			if ip != "127.0.0.1" && ip != "0.0.0.0" && !strings.HasPrefix(ip, "255.") {
				reportedIP = true
				add("low", "hardcoded-address", n, "Hard-coded IP address",
					fmt.Sprintf("`%s` ties this action to one environment.", ip),
					"Take the address from a parameter so the same action works elsewhere.")
			}
		}
		if m := reAptInstall.FindStringSubmatch(l); m != nil {
			usesApt = true
			if !strings.Contains(m[3], "-y") && !strings.Contains(m[3], "--yes") && !strings.Contains(m[3], "--assume-yes") && m[2] != "autoremove" {
				add("medium", "apt-interactive", n, "apt will wait for a confirmation that never comes",
					"Without `-y` apt asks \"Do you want to continue?\"; Forgemill runs scripts without a terminal, so the run hangs until it times out.",
					"Add `-y` (and keep `export DEBIAN_FRONTEND=noninteractive`).")
			}
		} else if reAptAny.MatchString(l) {
			usesApt = true
		}
		if m := reDnfInstall.FindStringSubmatch(l); m != nil {
			usesRhel = true
			if !strings.Contains(m[3], "-y") && !strings.Contains(m[3], "--assumeyes") {
				add("medium", "dnf-interactive", n, fmt.Sprintf("%s will wait for a confirmation that never comes", m[1]),
					"Without `-y` the package manager prompts; Forgemill runs scripts without a terminal, so the run hangs.",
					"Add `-y`.")
			}
		} else if reRhelAny.MatchString(l) {
			usesRhel = true
		}
		if reOSRelease.MatchString(l) {
			hasOSRelease = true
		}
		if reReboot.MatchString(t) {
			add("high", "reboots-host", n, "Reboots or powers off the VM",
				"The SSH session carrying this run ends when the host goes down, so the action is reported as failed and anything after this line never runs.",
				"Leave the reboot to the operator (note it in the description), or make it the very last line and expect a failed status.")
		}
		if reEvalVar.MatchString(l) {
			add("medium", "eval-variable", n, "eval on variable content",
				"`eval` runs whatever the variable contains as code; a parameter value becomes a command.",
				"Avoid eval; use arrays or indirect expansion (`${!name}`) instead.")
		}
		if reSudo.MatchString(t) {
			add("info", "sudo-inside", n, "sudo is unnecessary here",
				"Actions already run as root through Forgemill's own sudo; a nested sudo adds nothing and prompts if the user's sudo needs a password.",
				"Drop the `sudo`.")
		}
		if reInteractive.MatchString(t) {
			add("medium", "interactive-command", n, "Interactive command",
				"This command expects a terminal and will hang or fail without one.",
				"Use a non-interactive form (e.g. `chpasswd`, `debconf-set-selections`, editing files with sed/tee).")
		}
	}

	// parameters: referenced but not declared / declared but unused
	undeclared := []string{}
	for name, line := range used {
		if declared[name] || assigned[name] || shellEnv[name] {
			continue
		}
		undeclared = append(undeclared, fmt.Sprintf("%s (line %d)", name, line))
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		add("medium", "parameter-undeclared", 0, "Variables used but not declared as parameters",
			fmt.Sprintf("%s look like inputs but are not in the parameter list; with `set -u` the script aborts, without it they are empty.", strings.Join(undeclared, ", ")),
			"Declare them as parameters (they arrive as environment variables) or assign them in the script.")
	}
	for _, p := range params {
		if _, ok := used[p.Name]; !ok {
			add("low", "parameter-unused", 0, fmt.Sprintf("Parameter %s is never used", p.Name), "The script never references `$"+p.Name+"`.", "Remove the parameter or use it.")
		}
	}

	if usesApt && !hasNonInteractive {
		add("low", "apt-frontend", 0, "apt without DEBIAN_FRONTEND=noninteractive",
			"Some packages open a debconf dialog during install; without this variable the run can hang on it.",
			"Add `export DEBIAN_FRONTEND=noninteractive` near the top.")
	}

	distro := DistroSupport{Debian: true, RHEL: true}
	switch {
	case usesApt && !usesRhel && !hasOSRelease:
		distro = DistroSupport{Debian: true, RHEL: false, Notes: "Uses apt only — fails on RHEL-family guests (Rocky, Alma, CentOS, Fedora)."}
		add("info", "distro-debian-only", 0, "Debian/Ubuntu only", distro.Notes, "Branch on /etc/os-release (`ID_LIKE`) and use dnf for the RHEL family, or say so in the description.")
	case usesRhel && !usesApt && !hasOSRelease:
		distro = DistroSupport{Debian: false, RHEL: true, Notes: "Uses dnf/yum only — fails on Debian-family guests."}
		add("info", "distro-rhel-only", 0, "RHEL-family only", distro.Notes, "Branch on /etc/os-release and use apt for Debian/Ubuntu, or say so in the description.")
	case usesApt && usesRhel && !hasOSRelease:
		distro.Notes = "Calls both apt and dnf/yum without checking the distro — one of them will fail."
		add("medium", "distro-unbranched", 0, "Both package managers called without a distro check", distro.Notes, "Read /etc/os-release and branch on ID / ID_LIKE.")
	}

	sortFindings(findings)
	return findings, distro
}
