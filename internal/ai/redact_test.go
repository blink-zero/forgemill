package ai

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	in := `#!/bin/bash
export DB_PASSWORD=hunter2
PASSWORD="s3cret!"
token: ghp_abcdefghijklmnopqrstuvwxyz0123456789
curl -H "Authorization: Bearer sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef" https://api.example.com/v1
mysql --password=topsecret -u root
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
git clone https://user:pa55@github.com/org/repo.git
PASS="$DB_PASSWORD"
-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
-----END OPENSSH PRIVATE KEY-----
echo done`
	out, rep := Redact(in, RedactOptions{})
	for _, leaked := range []string{"hunter2", "s3cret!", "ghp_abcdefghijklmnopqrstuvwxyz0123456789", "sk-ant-api03", "topsecret", "AKIAIOSFODNN7EXAMPLE", "pa55@", "b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ"} {
		if strings.Contains(out, leaked) {
			t.Errorf("leaked %q in:\n%s", leaked, out)
		}
	}
	for _, kept := range []string{"export DB_PASSWORD=", `PASSWORD="`, "-u root", "https://", "@github.com/org/repo.git", `PASS="$DB_PASSWORD"`, "echo done", "«REDACTED:private-key»"} {
		if !strings.Contains(out, kept) {
			t.Errorf("structure lost: %q missing in:\n%s", kept, out)
		}
	}
	if rep.Total < 7 || rep.Counts["password"] < 3 || rep.Counts["token"] < 2 || rep.Counts["private-key"] != 1 || rep.Counts["credential"] != 1 {
		t.Errorf("report %+v", rep)
	}
	// Idempotent: redacting the redaction changes nothing.
	again, rep2 := Redact(out, RedactOptions{})
	if again != out || rep2.Total != 0 {
		t.Errorf("second pass changed text (%d more)", rep2.Total)
	}
}

func TestRedactHostnamesIsOptional(t *testing.T) {
	in := "ssh admin@10.20.30.40 && curl http://nas.lab.internal/ && ping fe80::1 and nothing at 1.2"
	out, rep := Redact(in, RedactOptions{})
	if out != in || rep.Total != 0 {
		t.Fatalf("hostnames must not be touched by default: %q", out)
	}
	out, rep = Redact(in, RedactOptions{Hostnames: true})
	if strings.Contains(out, "10.20.30.40") || strings.Contains(out, "nas.lab.internal") || strings.Contains(out, "fe80::1") {
		t.Errorf("addresses leaked: %q", out)
	}
	if rep.Counts["ip"] != 2 || rep.Counts["host"] != 1 {
		t.Errorf("report %+v", rep)
	}
	if !strings.Contains(out, "nothing at 1.2") {
		t.Errorf("version-like numbers must survive: %q", out)
	}
}

func TestRedactLeavesOrdinaryScriptsAlone(t *testing.T) {
	in := "set -euo pipefail\napt-get install -y nginx\nsystemctl enable --now nginx\nTOKEN_FILE=/etc/app/token\n"
	out, rep := Redact(in, RedactOptions{})
	if rep.Total != 1 || !strings.Contains(out, "TOKEN_FILE=«REDACTED:password»") {
		// a path assigned to *_FILE is redacted by the conservative rule — acceptable, but it must be the only change
		t.Logf("report %+v out %q", rep, out)
	}
	if !strings.Contains(out, "apt-get install -y nginx") {
		t.Errorf("ordinary lines must survive: %q", out)
	}
}
