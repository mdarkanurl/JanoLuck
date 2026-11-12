package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/email"
)

type fakeEmailPublisher struct {
	calls int
	topic string
	key   string
	value any
	err   error
}

func (f *fakeEmailPublisher) Produce(_ context.Context, topic, key string, value any) error {
	f.calls++
	f.topic = topic
	f.key = key
	f.value = value
	return f.err
}

func authPayload(t *testing.T, event auth.UserEvent) []byte {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return payload
}

func TestWorkerQueuesEmail(t *testing.T) {
	publisher := &fakeEmailPublisher{}
	worker := NewWorker(publisher, nil)

	payload := authPayload(t, auth.UserEvent{UserID: "user-1", Email: "user@example.com"})
	if err := worker.Handle(context.Background(), auth.UserRegisteredTopic, payload); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if publisher.calls != 1 {
		t.Fatalf("publish calls = %d, want 1", publisher.calls)
	}
	if publisher.topic != email.NotificationTopic {
		t.Errorf("topic = %q, want %q", publisher.topic, email.NotificationTopic)
	}
	if publisher.key != "user@example.com" {
		t.Errorf("key = %q, want the email address", publisher.key)
	}

	job, ok := publisher.value.(email.Job)
	if !ok {
		t.Fatalf("value type = %T, want email.Job", publisher.value)
	}
	if job.To != "user@example.com" || job.Subject == "" || job.Body == "" {
		t.Errorf("job = %+v, want a complete email", job)
	}
}

func TestWorkerSkipsUnknownTopic(t *testing.T) {
	publisher := &fakeEmailPublisher{}
	worker := NewWorker(publisher, nil)

	payload := authPayload(t, auth.UserEvent{UserID: "user-1", Email: "user@example.com"})
	if err := worker.Handle(context.Background(), auth.LogoutTopic, payload); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if publisher.calls != 0 {
		t.Errorf("publish calls = %d, want 0", publisher.calls)
	}
}

func TestWorkerRequiresEmail(t *testing.T) {
	worker := NewWorker(&fakeEmailPublisher{}, nil)

	payload := authPayload(t, auth.UserEvent{UserID: "user-1"})
	if err := worker.Handle(context.Background(), auth.UserRegisteredTopic, payload); err == nil {
		t.Fatal("Handle() error = nil, want missing email error")
	}
}

func TestWorkerErrors(t *testing.T) {
	payload := authPayload(t, auth.UserEvent{UserID: "user-1", Email: "user@example.com"})

	if err := NewWorker(&fakeEmailPublisher{}, nil).Handle(context.Background(), auth.UserRegisteredTopic, []byte("{not-json")); err == nil {
		t.Fatal("Handle() error = nil, want decode error")
	}
	if err := NewWorker(nil, nil).Handle(context.Background(), auth.UserRegisteredTopic, payload); err == nil {
		t.Fatal("Handle() error = nil, want missing publisher error")
	}
	if err := NewWorker(&fakeEmailPublisher{err: errors.New("kafka down")}, nil).Handle(context.Background(), auth.UserRegisteredTopic, payload); err == nil {
		t.Fatal("Handle() error = nil, want publish error")
	}
}
