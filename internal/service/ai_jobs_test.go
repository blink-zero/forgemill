package service

import (
	"errors"
	"testing"
	"time"

	"github.com/forgemill/forgemill/internal/ai"
)

func waitJob(t *testing.T, a *AIAssistService, id string, owner *int64) *AIJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j, err := a.GetJob(id, owner, true)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != "running" {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return nil
}

func TestDraftJobRunsInBackgroundWithStagesAndResult(t *testing.T) {
	a, _ := newAITestService(t)
	fp := &fakeAIProvider{answers: []string{goodDraftJSON, `{"summary":"ok","risk":"low","findings":[]}`}}
	enableAI(t, a, fp)
	owner := int64(7)
	job, err := a.StartDraftJob(ActionDraftInput{Prompt: "mount an NFS share at /data from parameters"}, "admin", &owner)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "running" || job.Kind != "draft" || job.ID == "" {
		t.Fatalf("start: %+v", job)
	}
	done := waitJob(t, a, job.ID, &owner)
	if done.Status != "done" || done.Draft == nil || done.Draft.Name != "Mount NFS share" || done.Draft.Review == nil || done.ElapsedMs < 0 || done.FinishedAt == nil {
		t.Fatalf("done: %+v", done)
	}
	// Another user cannot read it; the owner and admins can.
	other := int64(8)
	if _, err := a.GetJob(job.ID, &other, false); !errors.Is(err, ErrAIJobNotFound) {
		t.Errorf("other user must not see the job, got %v", err)
	}
	if _, err := a.GetJob(job.ID, &owner, false); err != nil {
		t.Errorf("owner: %v", err)
	}
	if _, err := a.GetJob("nope", &owner, true); !errors.Is(err, ErrAIJobNotFound) {
		t.Errorf("unknown id: %v", err)
	}
}

func TestReviewJobFailureCarriesStatusAndDraftJobRefusedWhenOff(t *testing.T) {
	a, _ := newAITestService(t)
	// Draft with AI off: refused at start, like the synchronous endpoint.
	if _, err := a.StartDraftJob(ActionDraftInput{Prompt: "install nginx and enable it"}, "admin", nil); !errors.Is(err, ai.ErrNotConfigured) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	// Review with AI off still works (lint only) as a job.
	job, err := a.StartReviewJob(ActionReviewInput{Script: "apt-get install nginx\n"}, "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := waitJob(t, a, job.ID, nil)
	if done.Status != "done" || done.Review == nil || !done.Review.LintOnly {
		t.Fatalf("lint-only review job: %+v", done)
	}
	// A provider failure inside a draft job becomes a failed job with 502.
	fp := &fakeAIProvider{err: &ai.ProviderError{Provider: "X", Status: 500, Message: "upstream down"}}
	enableAI(t, a, fp)
	job, err = a.StartDraftJob(ActionDraftInput{Prompt: "install nginx and enable it"}, "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	done = waitJob(t, a, job.ID, nil)
	if done.Status != "failed" || done.ErrorStatus != 502 || done.Error == "" || done.Draft != nil {
		t.Fatalf("failed job: %+v", done)
	}
	// Bad input is rejected before a job exists.
	if _, err := a.StartReviewJob(ActionReviewInput{Script: " "}, "admin", nil); !errors.Is(err, ErrAIInput) {
		t.Errorf("empty script: %v", err)
	}
}

func TestJobStoreSweepsExpiredAndCapsConcurrency(t *testing.T) {
	st := newAIJobStore()
	block := make(chan struct{})
	for i := 0; i < aiJobRunningLimit; i++ {
		if _, err := st.start("review", nil, func(func(string)) (*ActionReview, *ActionDraft, error) { <-block; return &ActionReview{}, nil, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.start("review", nil, func(func(string)) (*ActionReview, *ActionDraft, error) { return nil, nil, nil }); !errors.Is(err, ErrAIBusy) {
		t.Errorf("cap: want ErrAIBusy, got %v", err)
	}
	close(block)
	time.Sleep(50 * time.Millisecond)
	// Age one job past the TTL and sweep.
	st.mu.Lock()
	for _, j := range st.jobs {
		old := time.Now().Add(-aiJobTTL - time.Minute)
		j.FinishedAt = &old
		break
	}
	st.sweep(time.Now())
	n := len(st.jobs)
	st.mu.Unlock()
	if n != aiJobRunningLimit-1 {
		t.Errorf("sweep: %d jobs left, want %d", n, aiJobRunningLimit-1)
	}
}
