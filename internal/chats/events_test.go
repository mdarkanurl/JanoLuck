package chats

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

type fakePublisher struct {
	calls   int
	topics  []string
	topic   string
	key     string
	payload []byte
	headers map[string]string
	err     error
}

func (f *fakePublisher) Produce(context.Context, string, string, any) error {
	return nil
}

func (f *fakePublisher) ProduceAsync(context.Context, string, string, any) error {
	return nil
}

func (f *fakePublisher) ProduceRawAsync(_ context.Context, topic string, key, value []byte, headers map[string]string) error {
	f.calls++
	f.topics = append(f.topics, topic)
	f.topic = topic
	f.key = string(key)
	f.payload = value
	f.headers = headers
	return f.err
}

func TestServiceUpdateChatPublishesEventsWithoutStore(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.NewRows(chatColumns, chatRowValues()), nil
		},
	})

	publisher := &fakePublisher{}
	svc := NewService(repo, nil, WithProducer(publisher))

	if _, err := svc.UpdateChat(context.Background(), testUserID.String(), testChatID.String(), "updated"); err != nil {
		t.Fatalf("UpdateChat() error = %v", err)
	}
	if publisher.calls != 3 {
		t.Fatalf("publish calls = %d, want 3", publisher.calls)
	}

	topics := map[string]bool{}
	for _, topic := range publisher.topics {
		topics[topic] = true
	}
	if !topics["chat.embedding.requested"] || !topics["moderation.requested"] || !topics["analytics.events"] {
		t.Errorf("published topics = %v, want embedding, moderation and analytics", topics)
	}
}

func TestServiceUpdateChatFailsWhenProducerFails(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.NewRows(chatColumns, chatRowValues()), nil
		},
	})

	publisher := &fakePublisher{err: errors.New("kafka down")}
	svc := NewService(repo, nil, WithProducer(publisher))

	if _, err := svc.UpdateChat(context.Background(), testUserID.String(), testChatID.String(), "updated"); err != nil {
		t.Fatalf("UpdateChat() error = %v, want nil", err)
	}
}

func TestServiceUpdateChatUsesOutbox(t *testing.T) {
	var enqueued int
	db := &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "UpdateChatContent") {
				return testutil.NewRows(chatColumns, chatRowValues()), nil
			}
			if strings.Contains(query, "EnqueueOutbox") {
				enqueued++
				return testutil.NewRows(
					[]string{"id", "topic", "key", "payload", "headers", "create_at", "published_at"},
					[]driver.Value{int64(1), "moderation.requested", "key", []byte(`{}`), []byte(`{}`), testTime, nil},
				), nil
			}
			return testutil.QueryError(errors.New("unexpected query"))
		},
	}

	repo := testRepository(t, db)
	publisher := &fakePublisher{}
	store := outbox.NewRepository(testutil.NewQueries(t, db))
	svc := NewService(repo, nil, WithProducer(publisher), WithEventStore(store))

	if _, err := svc.UpdateChat(context.Background(), testUserID.String(), testChatID.String(), "updated"); err != nil {
		t.Fatalf("UpdateChat() error = %v", err)
	}
	if publisher.calls != 0 {
		t.Errorf("direct publish calls = %d, want 0 when the outbox is used", publisher.calls)
	}
	if enqueued != 3 {
		t.Errorf("outbox inserts = %d, want 3", enqueued)
	}
}

func TestServiceEnqueueSummary(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "CountChatsBySessionID") {
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(10)}), nil
			}
			return testutil.QueryError(errors.New("unexpected query"))
		},
	})

	publisher := &fakePublisher{}
	svc := NewService(repo, nil, WithProducer(publisher))

	svc.enqueueSummary(context.Background(), testSessionID, testUserID)

	if publisher.calls != 1 {
		t.Fatalf("publish calls = %d, want 1", publisher.calls)
	}
	if publisher.topic != "session.summary.requested" {
		t.Errorf("topic = %q, want session.summary.requested", publisher.topic)
	}

	var job map[string]string
	if err := json.Unmarshal(publisher.payload, &job); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if job["session_id"] != testSessionID.String() {
		t.Errorf("payload = %v", job)
	}
}

func TestServiceEnqueueSummarySkipsNonBatchBoundary(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "CountChatsBySessionID") {
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(7)}), nil
			}
			return testutil.QueryError(errors.New("unexpected query"))
		},
	})

	publisher := &fakePublisher{}
	svc := NewService(repo, nil, WithProducer(publisher))

	svc.enqueueSummary(context.Background(), testSessionID, testUserID)

	if publisher.calls != 0 {
		t.Errorf("publish calls = %d, want 0", publisher.calls)
	}
}
