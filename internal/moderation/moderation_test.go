package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeModerator struct {
	allowed bool
	reason  string
	err     error
	content string
}

func (f *fakeModerator) ModerateContentContext(_ context.Context, content string) (bool, string, error) {
	f.content = content
	return f.allowed, f.reason, f.err
}

type fakeStore struct {
	modeID   uuid.UUID
	chatID   uuid.UUID
	status   string
	reason   string
	err      error
	modeHits int
	chatHits int
}

func (f *fakeStore) SetModeStatus(_ context.Context, id uuid.UUID, status, reason string) error {
	f.modeHits++
	f.modeID = id
	f.status = status
	f.reason = reason
	return f.err
}

func (f *fakeStore) SetChatStatus(_ context.Context, id uuid.UUID, status, reason string) error {
	f.chatHits++
	f.chatID = id
	f.status = status
	f.reason = reason
	return f.err
}

func moderationPayload(t *testing.T, job Job) []byte {
	t.Helper()
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	return payload
}

func TestJobValidate(t *testing.T) {
	valid := Job{Kind: KindChat, ID: uuid.NewString(), Content: "hello"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	for _, job := range []Job{
		{Kind: "other", ID: uuid.NewString(), Content: "hello"},
		{Kind: KindChat, ID: "bad", Content: "hello"},
		{Kind: KindChat, ID: uuid.NewString(), Content: " "},
	} {
		if err := job.Validate(); err == nil {
			t.Errorf("Validate(%+v) error = nil, want error", job)
		}
	}
}

func TestWorkerAllowsMode(t *testing.T) {
	store := &fakeStore{}
	moderator := &fakeModerator{allowed: true}
	worker := NewWorker(store, moderator, nil)

	job := Job{Kind: KindMode, ID: uuid.NewString(), Content: "be kind"}
	if err := worker.Handle(context.Background(), moderationPayload(t, job)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if moderator.content != "be kind" {
		t.Errorf("moderator content = %q", moderator.content)
	}
	if store.modeHits != 1 || store.chatHits != 0 {
		t.Errorf("store calls = mode:%d chat:%d", store.modeHits, store.chatHits)
	}
	if store.status != StatusAllowed || store.reason != "" {
		t.Errorf("status/reason = %q/%q", store.status, store.reason)
	}
}

func TestWorkerBlocksChat(t *testing.T) {
	store := &fakeStore{}
	worker := NewWorker(store, &fakeModerator{allowed: false, reason: "hate speech"}, nil)

	job := Job{Kind: KindChat, ID: uuid.NewString(), Content: "bad"}
	if err := worker.Handle(context.Background(), moderationPayload(t, job)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if store.chatHits != 1 || store.modeHits != 0 {
		t.Errorf("store calls = mode:%d chat:%d", store.modeHits, store.chatHits)
	}
	if store.status != StatusBlocked || store.reason != "hate speech" {
		t.Errorf("status/reason = %q/%q", store.status, store.reason)
	}
}

func TestWorkerSkipsDeletedTarget(t *testing.T) {
	store := &fakeStore{err: ErrNotFound}
	worker := NewWorker(store, &fakeModerator{allowed: true}, nil)

	job := Job{Kind: KindChat, ID: uuid.NewString(), Content: "hello"}
	if err := worker.Handle(context.Background(), moderationPayload(t, job)); err != nil {
		t.Fatalf("Handle() error = %v, want nil for deleted target", err)
	}
}

func TestWorkerErrors(t *testing.T) {
	store := &fakeStore{}
	validJob := Job{Kind: KindChat, ID: uuid.NewString(), Content: "hello"}

	if err := NewWorker(store, &fakeModerator{}, nil).Handle(context.Background(), []byte("{not-json")); err == nil {
		t.Fatal("Handle() error = nil, want decode error")
	}
	if err := NewWorker(store, &fakeModerator{}, nil).Handle(context.Background(), moderationPayload(t, Job{})); err == nil {
		t.Fatal("Handle() error = nil, want invalid job error")
	}
	if err := NewWorker(nil, &fakeModerator{}, nil).Handle(context.Background(), moderationPayload(t, validJob)); err == nil {
		t.Fatal("Handle() error = nil, want missing store error")
	}
	if err := NewWorker(store, &fakeModerator{err: errors.New("llm down")}, nil).Handle(context.Background(), moderationPayload(t, validJob)); err == nil {
		t.Fatal("Handle() error = nil, want moderator error")
	}
	if err := NewWorker(&fakeStore{err: errors.New("db down")}, &fakeModerator{}, nil).Handle(context.Background(), moderationPayload(t, validJob)); err == nil {
		t.Fatal("Handle() error = nil, want store error")
	}
}
