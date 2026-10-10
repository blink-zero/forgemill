package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Background jobs for model-backed calls. The browser starts a job and polls
// its status with short requests, so no single HTTP request is held open
// for the length of a model call — nothing in front of Forgemill (a reverse
// proxy with default timeouts, a flaky mobile connection) can cut it off.
// Jobs live in memory for a few minutes; a restart forgets them, which the
// UI reports as "job not found — start again".

const (
	aiJobTTL          = 10 * time.Minute // kept after finishing
	aiJobRunningLimit = 8                // concurrent model calls per instance
	aiJobIDBytes      = 12
)

// AIJob is one review or draft in flight or recently finished.
type AIJob struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`   // review | draft
	Status     string     `json:"status"` // running | done | failed
	Stage      string     `json:"stage,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	ElapsedMs  int64      `json:"elapsed_ms"`
	// Exactly one of these is set once Status is done / failed.
	Review *ActionReview `json:"review,omitempty"`
	Draft  *ActionDraft  `json:"draft,omitempty"`
	Error  string        `json:"error,omitempty"`
	// ErrorStatus is the HTTP status the synchronous endpoint would have
	// returned for Error, so the UI can tell 409 (AI off) from 502.
	ErrorStatus int `json:"error_status,omitempty"`

	ownerID *int64
	err     error
}

// ErrAIJobNotFound: unknown or expired job id (or not the caller's job).
var ErrAIJobNotFound = errors.New("job not found or expired — start it again")

// ErrAIBusy: too many model calls already running on this instance.
var ErrAIBusy = errors.New("too many AI requests running right now; try again in a moment")

type aiJobStore struct {
	mu   sync.Mutex
	jobs map[string]*AIJob
}

func newAIJobStore() *aiJobStore { return &aiJobStore{jobs: map[string]*AIJob{}} }

func newJobID() string {
	b := make([]byte, aiJobIDBytes)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// sweep drops finished jobs past their TTL. Called under the lock.
func (st *aiJobStore) sweep(now time.Time) {
	for id, j := range st.jobs {
		if j.FinishedAt != nil && now.Sub(*j.FinishedAt) > aiJobTTL {
			delete(st.jobs, id)
		}
	}
}

func (st *aiJobStore) running() int {
	n := 0
	for _, j := range st.jobs {
		if j.Status == "running" {
			n++
		}
	}
	return n
}

// start registers a job and runs fn in the background. fn receives a stage
// reporter and returns the finished job's payload via the setters.
func (st *aiJobStore) start(kind string, ownerID *int64, run func(stage func(string)) (review *ActionReview, draft *ActionDraft, err error)) (*AIJob, error) {
	st.mu.Lock()
	st.sweep(time.Now())
	if st.running() >= aiJobRunningLimit {
		st.mu.Unlock()
		return nil, ErrAIBusy
	}
	job := &AIJob{ID: newJobID(), Kind: kind, Status: "running", Stage: "queued", StartedAt: time.Now(), ownerID: ownerID}
	st.jobs[job.ID] = job
	st.mu.Unlock()

	go func() {
		stage := func(s string) {
			st.mu.Lock()
			job.Stage = s
			st.mu.Unlock()
		}
		review, draft, err := run(stage)
		st.mu.Lock()
		defer st.mu.Unlock()
		now := time.Now()
		job.FinishedAt = &now
		job.Stage = ""
		if err != nil {
			job.Status = "failed"
			job.err = err
			job.Error = userFacingAIError(err)
			job.ErrorStatus = aiErrorStatus(err)
			return
		}
		job.Status = "done"
		job.Review, job.Draft = review, draft
	}()
	return snapshot(job), nil
}

// get returns a copy of the job for ownerID (admins may read any).
func (st *aiJobStore) get(id string, ownerID *int64, admin bool) (*AIJob, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweep(time.Now())
	j, ok := st.jobs[id]
	if !ok {
		return nil, ErrAIJobNotFound
	}
	if !admin && (j.ownerID == nil || ownerID == nil || *j.ownerID != *ownerID) {
		return nil, ErrAIJobNotFound
	}
	return snapshot(j), nil
}

// snapshot copies a job for return outside the lock (callers hold it).
func snapshot(j *AIJob) *AIJob {
	c := *j
	end := time.Now()
	if j.FinishedAt != nil {
		end = *j.FinishedAt
	}
	c.ElapsedMs = end.Sub(j.StartedAt).Milliseconds()
	return &c
}

// aiErrorStatus mirrors writeAIError's mapping so a job can carry it.
func aiErrorStatus(err error) int {
	switch {
	case errors.Is(err, ErrAIInput):
		return 400
	case errors.Is(err, ErrAIRateLimited), errors.Is(err, ErrAIBusy):
		return 429
	}
	if errors.Is(err, errNotConfiguredSentinel()) {
		return 409
	}
	if isProviderError(err) {
		return 502
	}
	return 500
}

// StartReviewJob / StartDraftJob run the model-backed work in the
// background under a fresh context with the usual budget.
func (s *AIAssistService) StartReviewJob(in ActionReviewInput, actor string, actorID *int64) (*AIJob, error) {
	if err := validateReviewInput(&in); err != nil {
		return nil, err
	}
	return s.jobs.start("review", actorID, func(stage func(string)) (*ActionReview, *ActionDraft, error) {
		stage("reviewing")
		ctx, cancel := context.WithTimeout(context.Background(), aiRequestBudget)
		defer cancel()
		r, err := s.ReviewAction(ctx, in, actor, actorID)
		return r, nil, err
	})
}

func (s *AIAssistService) StartDraftJob(in ActionDraftInput, actor string, actorID *int64) (*AIJob, error) {
	if err := validateDraftInput(&in); err != nil {
		return nil, err
	}
	// Refuse up front when AI is off: the synchronous endpoint returns 409
	// before doing anything, and a job should not be created only to fail.
	if _, _, err := s.provider(); err != nil {
		return nil, err
	}
	return s.jobs.start("draft", actorID, func(stage func(string)) (*ActionReview, *ActionDraft, error) {
		ctx, cancel := context.WithTimeout(context.Background(), aiRequestBudget)
		defer cancel()
		d, err := s.draftAction(ctx, in, actor, actorID, stage)
		return nil, d, err
	})
}

// GetJob returns a job's current state.
func (s *AIAssistService) GetJob(id string, actorID *int64, admin bool) (*AIJob, error) {
	return s.jobs.get(id, actorID, admin)
}

// aiRequestBudget bounds one job end to end (draft + review, with retries).
const aiRequestBudget = 5 * time.Minute
