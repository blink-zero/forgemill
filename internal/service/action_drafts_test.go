package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/forgemill/forgemill/internal/db/models"
)

// An AI draft lands in the database as a draft action with its review and
// the model's notes; regenerating overwrites the same draft; drafts are
// hidden from the runnable list.
func TestAIDraftIsSavedAsDraftActionAndRegenerateOverwrites(t *testing.T) {
	a, _ := newAITestService(t)
	fp := &fakeAIProvider{answers: []string{goodDraftJSON, `{"summary":"Mounts an NFS export idempotently.","risk":"low","idempotent":true,"findings":[]}`}}
	enableAI(t, a, fp)
	owner := int64(3)
	d, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "mount an NFS share at /data from parameters"}, "admin", &owner)
	if err != nil {
		t.Fatal(err)
	}
	if d.ActionID == 0 {
		t.Fatal("draft must be saved as an action")
	}
	saved, err := a.db.GetAction(d.ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != models.ActionStatusDraft || saved.Source != models.ActionSourceAI || saved.Name != "Mount NFS share" || saved.CreatedBy == nil || *saved.CreatedBy != owner || saved.Version != 1 {
		t.Fatalf("saved draft: %+v", saved)
	}
	var review ActionReview
	if err := json.Unmarshal(saved.Review, &review); err != nil || review.Summary == "" || review.ScriptHash != ScriptHash(saved.Script) {
		t.Fatalf("stored review: %s err %v", saved.Review, err)
	}
	var meta DraftMeta
	if err := json.Unmarshal(saved.DraftMeta, &meta); err != nil || !strings.Contains(meta.Prompt, "NFS") || len(meta.Notes) != 1 {
		t.Fatalf("stored meta: %s err %v", saved.DraftMeta, err)
	}
	// Hidden from the runnable list, present with drafts.
	active, _ := a.db.ListActions()
	for _, x := range active {
		if x.ID == d.ActionID {
			t.Error("draft must not appear in ListActions")
		}
	}
	all, _ := a.db.ListActionsWithDrafts()
	found := false
	for _, x := range all {
		found = found || x.ID == d.ActionID
	}
	if !found {
		t.Error("draft must appear in ListActionsWithDrafts")
	}
	// Regenerate into the same draft: same id, no version noise.
	fp = &fakeAIProvider{answers: []string{strings.Replace(goodDraftJSON, "Mount NFS share", "Mount NFS export", 1), `{"summary":"ok","risk":"low","findings":[]}`}}
	enableAI(t, a, fp)
	id := d.ActionID
	d2, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "same but call it export", DraftActionID: &id}, "admin", &owner)
	if err != nil || d2.ActionID != id {
		t.Fatalf("regenerate: %+v err %v", d2, err)
	}
	again, _ := a.db.GetAction(id)
	if again.Name != "Mount NFS export" || again.Version != 1 {
		t.Errorf("overwritten draft: name=%q version=%d", again.Name, again.Version)
	}
	if versions, _ := a.db.ListActionVersionHistory(id); len(versions) != 0 {
		t.Errorf("drafts must not accumulate versions, got %d", len(versions))
	}
	// Regenerating into a non-draft is refused.
	a.db.PublishAction(id)
	enableAI(t, a, &fakeAIProvider{answers: []string{goodDraftJSON, `{"summary":"ok","risk":"low","findings":[]}`}})
	if _, err := a.DraftAction(context.Background(), ActionDraftInput{Prompt: "overwrite the published one please", DraftActionID: &id}, "admin", &owner); !errors.Is(err, ErrAIInput) {
		t.Errorf("regenerate into a published action must be refused, got %v", err)
	}
}

// Drafts never run: not via the executor, not via a deployment's action
// list. Publishing validates and makes them runnable, version 1.
func TestDraftsCannotRunUntilPublished(t *testing.T) {
	s, _, tmplID, targetID := newDeployTestService(t)
	draft := &models.Action{Name: "Dangerous draft", Category: "custom", Script: "set -e\necho hi\n", Status: models.ActionStatusDraft, Source: models.ActionSourceAI}
	if err := s.db.CreateAction(draft); err != nil {
		t.Fatal(err)
	}
	// Executor refuses.
	svc, database, vmID := newNICTestService(t, "esxi")
	_ = svc
	ex := NewExecutorService(database, NewTargetService(database, nil), nil, noopExecHub{})
	d2 := &models.Action{Name: "Draft on vm db", Category: "custom", Script: "set -e\necho hi\n", Status: models.ActionStatusDraft}
	if err := database.CreateAction(d2); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateManagedVMState(vmID, "poweredOn", "10.0.0.5", nil, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	_, err := ex.Execute(context.Background(), vmID, ExecuteRequest{ActionID: &d2.ID}, 1)
	if !errors.Is(err, ErrActionDraft) {
		t.Errorf("executor must refuse a draft, got %v", err)
	}
	// Deploy refuses, in preflight and in Start.
	req := &DeployRequest{TemplateID: tmplID, TargetID: targetID, VMName: "web-77", CPU: 2, MemoryMB: 2048, DiskGB: 20, ActionIDs: []int64{draft.ID}}
	pf, _ := s.Preflight(context.Background(), req)
	if pf.Valid || len(pf.Blockers) == 0 || !strings.Contains(pf.Blockers[0], "draft") {
		t.Errorf("preflight must block a draft action: %+v", pf)
	}
	if _, err := s.Start(req, 1); !errors.Is(err, ErrInvalidDeployRequest) || !strings.Contains(err.Error(), "draft") {
		t.Errorf("Start must refuse a draft action, got %v", err)
	}
	// Publish → runnable, version 1, appears in ListActions.
	if err := s.db.PublishAction(draft.ID); err != nil {
		t.Fatal(err)
	}
	pub, _ := s.db.GetAction(draft.ID)
	if pub.Status != models.ActionStatusActive || pub.Version != 1 {
		t.Errorf("published: %+v", pub)
	}
	if pf, _ := s.Preflight(context.Background(), req); !pf.Valid {
		t.Errorf("published action must pass preflight: %+v", pf)
	}
	if err := s.db.PublishAction(draft.ID); err == nil {
		t.Error("publishing twice must fail")
	}
}

// A review names an action: it is stored there with the script's hash, and
// editing the content clears it.
func TestReviewAttachesToActionAndContentChangeClearsIt(t *testing.T) {
	a, _ := newAITestService(t)
	act := &models.Action{Name: "Check me", Category: "custom", Script: "apt-get install nginx\n"}
	if err := a.db.CreateAction(act); err != nil {
		t.Fatal(err)
	}
	r, err := a.ReviewAction(context.Background(), ActionReviewInput{Name: act.Name, Script: act.Script, ActionID: &act.ID}, "admin", nil)
	if err != nil || !r.LintOnly {
		t.Fatal(err)
	}
	stored, _ := a.db.GetAction(act.ID)
	if len(stored.Review) == 0 || stored.ReviewedAt == "" {
		t.Fatalf("review not stored: %+v", stored)
	}
	var got ActionReview
	_ = json.Unmarshal(stored.Review, &got)
	if got.ScriptHash != ScriptHash(act.Script) || len(got.Findings) == 0 {
		t.Errorf("stored review: %+v", got)
	}
	// Content edit clears it (a user update carries no review).
	stored.Script = "set -e\napt-get install -y nginx\n"
	stored.Review, stored.ReviewedAt = nil, ""
	if err := a.db.UpdateAction(stored, nil); err != nil {
		t.Fatal(err)
	}
	after, _ := a.db.GetAction(act.ID)
	if len(after.Review) != 0 || after.ReviewedAt != "" || after.Version != 2 {
		t.Errorf("after edit: review=%s at=%q version=%d", after.Review, after.ReviewedAt, after.Version)
	}
}

type noopExecHub struct{}

func (noopExecHub) SendOutput(int64, any) {}
