package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/auth"
)

type fakeRecorder struct {
	calls     int
	eventType string
	event     auth.UserEvent
	err       error
}

func (f *fakeRecorder) Record(_ context.Context, eventType string, event auth.UserEvent) error {
	f.calls++
	f.eventType = eventType
	f.event = event
	return f.err
}

func auditPayload(t *testing.T, event auth.UserEvent) []byte {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return payload
}

func TestWorkerRecordsEvent(t *testing.T) {
	recorder := &fakeRecorder{}
	worker := NewWorker(recorder, nil)

	event := auth.UserEvent{UserID: "user-1", Email: "user@example.com"}
	if err := worker.Handle(context.Background(), auth.LoginSucceededTopic, auditPayload(t, event)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if recorder.calls != 1 {
		t.Fatalf("record calls = %d, want 1", recorder.calls)
	}
	if recorder.eventType != auth.LoginSucceededTopic {
		t.Errorf("event type = %q, want %q", recorder.eventType, auth.LoginSucceededTopic)
	}
	if recorder.event.UserID != "user-1" {
		t.Errorf("user id = %q, want user-1", recorder.event.UserID)
	}
}

func TestWorkerErrors(t *testing.T) {
	payload := auditPayload(t, auth.UserEvent{UserID: "user-1"})

	if err := NewWorker(&fakeRecorder{}, nil).Handle(context.Background(), auth.LogoutTopic, []byte("{not-json")); err == nil {
		t.Fatal("Handle() error = nil, want decode error")
	}
	if err := NewWorker(&fakeRecorder{}, nil).Handle(context.Background(), auth.LogoutTopic, auditPayload(t, auth.UserEvent{})); err == nil {
		t.Fatal("Handle() error = nil, want missing user id error")
	}
	if err := NewWorker(nil, nil).Handle(context.Background(), auth.LogoutTopic, payload); err == nil {
		t.Fatal("Handle() error = nil, want missing recorder error")
	}
	if err := NewWorker(&fakeRecorder{err: errors.New("db down")}, nil).Handle(context.Background(), auth.LogoutTopic, payload); err == nil {
		t.Fatal("Handle() error = nil, want record error")
	}
}
