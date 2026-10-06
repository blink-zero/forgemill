package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/ai"
	"github.com/forgemill/forgemill/internal/db/models"
)

const goodDraftJSON = `{"name":"Mount NFS share","description":"Mounts an NFS export and persists it in fstab.","category":"Scripts",
 "script":"#!/bin/bash\nset -euo pipefail\n: \"${NFS_SERVER:?}\"\n: \"${NFS_EXPORT:?}\"\n: \"${MOUNT_POINT:?}\"\nmkdir -p \"$MOUNT_POINT\"\ngrep -q \"$MOUNT_POINT\" /etc/fstab || echo \"$NFS_SERVER:$NFS_EXPORT $MOUNT_POINT nfs defaults 0 0\" >> /etc/fstab\nmount -a\n",
 "parameters":[{"name":"nfs_server","label":"NFS server","type":"string","required":true},{"name":"NFS_EXPORT","type":"string","required":true},{"name":"MOUNT_POINT","label":"Mount point","type":"weird","default":"/data"},{"name":"not valid!","type":"string"}],
 "tags":["NFS","storage","storage","Bad Tag!"],"notes":["Needs nfs-common / nfs-utils installed."],"warnings":[]}`

func TestDraftActionNeedsAIOn(t *testing.T) {
	a, _ := newAITestService(t)
	_, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "mount an nfs share at /data"}, "admin", nil)
	if !errors.Is(err, ai.ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	if _, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "short"}, "admin", nil); !errors.Is(err, ErrAIInput) {
		t.Errorf("tiny prompt must be rejected, got %v", err)
	}
}

func TestDraftActionValidatesSanitisesAndReviews(t *testing.T) {
	a, _ := newAITestService(t)
	// First answer: the draft. Second: the review of that draft.
	fp := &fakeAIProvider{answers: []string{goodDraftJSON, `{"summary":"Mounts an NFS export idempotently.","risk":"low","idempotent":true,"findings":[]}`}}
	enableAI(t, a, fp)
	d, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "mount an NFS share at /data from a server given as parameters"}, "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Refused || d.Name != "Mount NFS share" || d.Category != "scripts" || !strings.HasPrefix(d.Script, "#!/bin/bash\nset -euo pipefail") || !strings.HasSuffix(d.Script, "\n") {
		t.Fatalf("draft: %+v", d)
	}
	if len(d.Parameters) != 3 || d.Parameters[0].Name != "NFS_SERVER" || d.Parameters[1].Label == "" || d.Parameters[2].Type != "string" {
		t.Errorf("parameters: %+v", d.Parameters)
	}
	if len(d.Tags) != 2 || d.Tags[0] != "nfs" || d.Tags[1] != "storage" {
		t.Errorf("tags: %v", d.Tags)
	}
	if d.Review == nil || d.Review.LintOnly || d.Review.Summary == "" {
		t.Errorf("draft must come with its review: %+v", d.Review)
	}
	if len(fp.calls) != 2 || fp.calls[0].System != draftSystemPrompt || !strings.Contains(fp.calls[0].User, "mount an NFS share") {
		t.Errorf("calls: %d", len(fp.calls))
	}
	if d.Model != "fake-1" || d.DurationMs < 0 {
		t.Errorf("meta: %+v", d)
	}
}

func TestDraftActionRefusalAndBadScript(t *testing.T) {
	a, _ := newAITestService(t)
	fp := &fakeAIProvider{answers: []string{`{"name":"","script":"","warnings":["Wiping all disks on every host is not something I will draft."]}`}}
	enableAI(t, a, fp)
	d, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "wipe every disk on every host"}, "admin", nil)
	if err != nil || !d.Refused || len(d.Warnings) != 1 || d.Review != nil {
		t.Fatalf("refusal: %+v err %v", d, err)
	}
	// An existing script is redacted before it is sent.
	fp = &fakeAIProvider{answers: []string{goodDraftJSON, `{"summary":"ok","risk":"low","findings":[]}`}}
	enableAI(t, a, fp)
	_, err = a.DraftAction(context.Background(), ActionDraftInput{Prompt: "make this idempotent and add a mount point parameter", ExistingScript: "mount x:/y /data\nPASSWORD=hunter2\n", ExistingParameters: []models.ActionParameter{{Name: "X", Type: "string"}}}, "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fp.calls[0].User, "hunter2") || !strings.Contains(fp.calls[0].User, "Start from this existing script") || !strings.Contains(fp.calls[0].User, "- X (string)") {
		t.Errorf("prompt:\n%s", fp.calls[0].User)
	}
	// Oversized script from the model is refused.
	fp = &fakeAIProvider{answers: []string{`{"name":"big","script":"` + strings.Repeat("echo x\\n", 12000) + `"}`}}
	enableAI(t, a, fp)
	if _, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "print x many times please"}, "admin", nil); err == nil || !strings.Contains(err.Error(), "not acceptable") {
		t.Errorf("oversized draft must be refused, got %v", err)
	}
}
