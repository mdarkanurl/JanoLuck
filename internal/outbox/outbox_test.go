package outbox

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

type fakeStore struct {
	pending   []Message
	published []int64
	listErr   error
	markErr   error
}

func (f *fakeStore) ListPending(_ context.Context, _ int) ([]Message, error) {
	return f.pending, f.listErr
}

func (f *fakeStore) MarkPublished(_ context.Context, id int64) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.published = append(f.published, id)
	return nil
}

type fakePublisher struct {
	calls   int
	topic   string
	key     string
	payload []byte
	headers map[string]string
	err     error
}

func (f *fakePublisher) ProduceRaw(_ context.Context, topic string, key, value []byte, headers map[string]string) error {
	f.calls++
	f.topic = topic
	f.key = string(key)
	f.payload = value
	f.headers = headers
	return f.err
}

func TestRelayRunOncePublishesAndMarks(t *testing.T) {
	store := &fakeStore{pending: []Message{
		{ID: 1, Topic: "a", Key: "k1", Payload: []byte(`{"n":1}`), Headers: map[string]string{"x": "y"}},
		{ID: 2, Topic: "b", Key: "k2", Payload: []byte(`{"n":2}`)},
	}}
	publisher := &fakePublisher{}
	relay := NewRelay(store, publisher)

	if err := relay.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if publisher.calls != 2 {
		t.Errorf("publish calls = %d, want 2", publisher.calls)
	}
	if len(store.published) != 2 || store.published[0] != 1 || store.published[1] != 2 {
		t.Errorf("published ids = %v, want [1 2]", store.published)
	}
	if publisher.topic != "b" || publisher.key != "k2" {
		t.Errorf("last publish = %q/%q", publisher.topic, publisher.key)
	}
}

func TestRelayRunOnceErrors(t *testing.T) {
	publisher := &fakePublisher{}
	if err := NewRelay(&fakeStore{listErr: errors.New("db down")}, publisher).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want list error")
	}

	store := &fakeStore{pending: []Message{{ID: 1, Topic: "a"}}}
	if err := NewRelay(store, &fakePublisher{err: errors.New("kafka down")}).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want publish error")
	}
	if len(store.published) != 0 {
		t.Errorf("published ids = %v, want none", store.published)
	}

	store = &fakeStore{pending: []Message{{ID: 1, Topic: "a"}}, markErr: errors.New("db down")}
	if err := NewRelay(store, &fakePublisher{}).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want mark error")
	}

	if err := NewRelay(nil, nil).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want configuration error")
	}
}

func TestRepositoryEnqueue(t *testing.T) {
	var topic, key string
	var payload, headers []byte
	repo := NewRepository(testutil.NewQueries(t, &testutil.DB{
		Query: func(_ string, args []driver.NamedValue) (driver.Rows, error) {
			topic, _ = args[0].Value.(string)
			key, _ = args[1].Value.(string)
			payload, _ = args[2].Value.([]byte)
			headers, _ = args[3].Value.([]byte)
			return testutil.NewRows(
				[]string{"id", "topic", "key", "payload", "headers", "create_at", "published_at"},
				[]driver.Value{int64(1), topic, key, payload, headers, time.Now(), nil},
			), nil
		},
	}))

	if err := repo.Enqueue(context.Background(), "analytics.events", "user-1", map[string]any{"n": 1}, map[string]string{"x": "y"}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if topic != "analytics.events" || key != "user-1" {
		t.Errorf("enqueued topic/key = %q/%q", topic, key)
	}
	if !strings.Contains(string(payload), `"n":1`) {
		t.Errorf("payload = %s", payload)
	}
	if !strings.Contains(string(headers), `"x":"y"`) {
		t.Errorf("headers = %s", headers)
	}
}

func TestRepositoryListPending(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"n": 1})
	headers, _ := json.Marshal(map[string]string{"x": "y"})

	repo := NewRepository(testutil.NewQueries(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.NewRows(
				[]string{"id", "topic", "key", "payload", "headers", "create_at"},
				[]driver.Value{int64(7), "analytics.events", "user-1", payload, headers, time.Now()},
			), nil
		},
	}))

	messages, err := repo.ListPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPending() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if messages[0].ID != 7 || messages[0].Topic != "analytics.events" || messages[0].Headers["x"] != "y" {
		t.Errorf("message = %+v", messages[0])
	}
}
