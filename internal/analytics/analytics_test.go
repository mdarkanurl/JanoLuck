package analytics

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakePublisher struct {
	calls int
	topic string
	key   string
	value any
	err   error
}

func (f *fakePublisher) ProduceAsync(_ context.Context, topic, key string, value any) error {
	f.calls++
	f.topic = topic
	f.key = key
	f.value = value
	return f.err
}

func TestRecordPublishesEvent(t *testing.T) {
	publisher := &fakePublisher{}
	before := time.Now().UTC()

	Record(context.Background(), publisher, Event{Type: "chat.created", UserID: "user-1"})

	if publisher.calls != 1 {
		t.Fatalf("publish calls = %d, want 1", publisher.calls)
	}
	if publisher.topic != Topic {
		t.Errorf("topic = %q, want %q", publisher.topic, Topic)
	}
	if publisher.key != "user-1" {
		t.Errorf("key = %q, want user-1", publisher.key)
	}

	event, ok := publisher.value.(Event)
	if !ok {
		t.Fatalf("value type = %T, want Event", publisher.value)
	}
	if event.OccurredAt.Before(before) {
		t.Errorf("occurred_at = %v, want >= %v", event.OccurredAt, before)
	}
}

func TestRecordNilPublisher(t *testing.T) {
	Record(context.Background(), nil, Event{Type: "chat.created"})
}

func TestRecordFallsBackToTypeKey(t *testing.T) {
	publisher := &fakePublisher{}
	Record(context.Background(), publisher, Event{Type: "mode.created"})

	if publisher.key != "mode.created" {
		t.Errorf("key = %q, want mode.created", publisher.key)
	}
}

func TestWorkerHandle(t *testing.T) {
	payload, err := json.Marshal(Event{Type: "chat.created", UserID: "user-1", Properties: map[string]any{"n": 1.0}})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	if err := NewWorker(nil).Handle(context.Background(), payload); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
}

func TestWorkerHandleErrors(t *testing.T) {
	worker := NewWorker(nil)

	if err := worker.Handle(context.Background(), []byte("{not-json")); err == nil {
		t.Fatal("Handle() error = nil, want decode error")
	}
	if err := worker.Handle(context.Background(), []byte(`{"type":""}`)); err == nil {
		t.Fatal("Handle() error = nil, want missing type error")
	}
}
