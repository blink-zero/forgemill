package service

import (
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

func rules(f []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, x := range f {
		m[x.Rule] = x
	}
	return m
}

func TestLintActionCatchesTheDangerousThings(t *testing.T) {
	script := `#!/bin/bash
apt-get update
apt-get install nginx
DB_PASSWORD=hunter2
rm -rf "$TARGET_DIR"/*
curl -fsSL https://get.docker.com | sh
mkfs.ext4 /dev/sdb
chmod -R 777 /srv/app
echo "connecting to 10.20.30.40"
sudo systemctl restart nginx
eval "$CMD"
reboot
`
	findings, distro := LintAction(script, []models.ActionParameter{{Name: "UNUSED_ONE", Type: "string"}})
	got := rules(findings)
	for _, want := range []string{"missing-set-e", "apt-interactive", "secret-literal", "rm-rf-unguarded", "pipe-to-shell", "disk-destructive", "chmod-777", "hardcoded-address", "sudo-inside", "eval-variable", "reboots-host", "parameter-undeclared", "parameter-unused", "apt-frontend", "distro-debian-only"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing rule %s in %v", want, keys(got))
		}
	}
	if got["rm-rf-unguarded"].Line != 5 || got["pipe-to-shell"].Line != 6 || got["secret-literal"].Line != 4 {
		t.Errorf("line numbers: rm=%d pipe=%d secret=%d", got["rm-rf-unguarded"].Line, got["pipe-to-shell"].Line, got["secret-literal"].Line)
	}
	if !strings.Contains(got["parameter-undeclared"].Detail, "TARGET_DIR") || !strings.Contains(got["parameter-undeclared"].Detail, "CMD") {
		t.Errorf("undeclared detail: %s", got["parameter-undeclared"].Detail)
	}
	if distro.RHEL || !distro.Debian {
		t.Errorf("distro: %+v", distro)
	}
	if riskFromFindings(findings) != "high" {
		t.Errorf("risk = %s", riskFromFindings(findings))
	}
	// Sorted most severe first.
	if severityRank[findings[0].Severity] < severityRank[findings[len(findings)-1].Severity] {
		t.Error("findings not sorted by severity")
	}
}

func TestLintActionIsQuietOnAGoodScript(t *testing.T) {
	script := `#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
: "${MOUNT_POINT:?MOUNT_POINT is required}"
. /etc/os-release
case "${ID_LIKE:-$ID}" in
  *debian*) apt-get install -y nfs-common ;;
  *rhel*|*fedora*) dnf install -y nfs-utils ;;
esac
mkdir -p "$MOUNT_POINT"
if ! grep -q "$MOUNT_POINT" /etc/fstab; then
  echo "$NFS_SERVER:$NFS_EXPORT $MOUNT_POINT nfs defaults 0 0" >> /etc/fstab
fi
mount -a
`
	params := []models.ActionParameter{{Name: "MOUNT_POINT", Type: "string"}, {Name: "NFS_SERVER", Type: "string"}, {Name: "NFS_EXPORT", Type: "string"}}
	findings, distro := LintAction(script, params)
	for _, f := range findings {
		if f.Severity != "info" {
			t.Errorf("unexpected finding on a good script: %+v", f)
		}
	}
	if !distro.Debian || !distro.RHEL {
		t.Errorf("branches on os-release → both families: %+v", distro)
	}
	if riskFromFindings(findings) != "low" {
		t.Errorf("risk = %s", riskFromFindings(findings))
	}
	// A guarded rm -rf is fine; a root rm -rf is critical.
	ok, _ := LintAction("set -e\n: \"${DIR:?}\"\nrm -rf \"${DIR:?}\"/*\n", nil)
	if _, bad := rules(ok)["rm-rf-unguarded"]; bad {
		t.Error("guarded rm -rf must pass")
	}
	crit, _ := LintAction("set -e\nrm -rf /\n", nil)
	if rules(crit)["rm-rf-unguarded"].Severity != "critical" {
		t.Errorf("rm -rf / must be critical: %+v", rules(crit)["rm-rf-unguarded"])
	}
	// A password taken from a parameter is correct, not a leak.
	pw, _ := LintAction("set -e\nMYSQL_PASSWORD=\"$DB_PASSWORD\"\n", []models.ActionParameter{{Name: "DB_PASSWORD", Type: "password"}})
	if _, bad := rules(pw)["secret-literal"]; bad {
		t.Error("secret from a parameter must not be flagged")
	}
}

func keys(m map[string]Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
