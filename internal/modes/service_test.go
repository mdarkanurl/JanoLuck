package modes

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

var (
	testUserID  = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testModeID  = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	testTime    = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	modeColumns = []string{"id", "userId", "name", "prompt", "isBuiltIn", "create_at", "update_at", "moderation_status", "moderation_reason"}
)

func modeRowValues() []driver.Value {
	return []driver.Value{
		testModeID.String(),
		testUserID.String(),
		"Motivation",
		"Be uplifting",
		false,
		testTime,
		testTime,
		"pending",
		nil,
	}
}

func testRepository(t *testing.T, db *testutil.DB) *Repository {
	t.Helper()
	return NewRepository(testutil.NewQueries(t, db))
}

type fakePublisher struct {
	calls  int
	topics []string
	err    error
}

func (f *fakePublisher) Produce(context.Context, string, string, any) error {
	return nil
}

func (f *fakePublisher) ProduceAsync(context.Context, string, string, any) error {
	return nil
}

func (f *fakePublisher) ProduceRawAsync(_ context.Context, topic string, _ []byte, _ []byte, _ map[string]string) error {
	f.calls++
	f.topics = append(f.topics, topic)
	return f.err
}

func TestServiceCreateModePublishesEvents(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.NewRows(modeColumns, modeRowValues()), nil
		},
	})

	publisher := &fakePublisher{}
	svc := NewService(repo, WithProducer(publisher))

	mode, err := svc.CreateMode(context.Background(), testUserID.String(), "Motivation", "Be uplifting")
	if err != nil {
		t.Fatalf("CreateMode() error = %v", err)
	}
	if mode.ID != testModeID {
		t.Errorf("mode id = %s, want %s", mode.ID, testModeID)
	}
	if publisher.calls != 2 {
		t.Fatalf("publish calls = %d, want 2", publisher.calls)
	}

	topics := map[string]bool{}
	for _, topic := range publisher.topics {
		topics[topic] = true
	}
	if !topics["moderation.requested"] || !topics["analytics.events"] {
		t.Errorf("published topics = %v, want moderation and analytics", topics)
	}
}

func TestServiceCreateModeUsesOutbox(t *testing.T) {
	var enqueued int
	db := &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "EnqueueOutbox") {
				enqueued++
				return testutil.NewRows(
					[]string{"id", "topic", "key", "payload", "headers", "create_at", "published_at"},
					[]driver.Value{int64(1), "moderation.requested", "key", []byte(`{}`), []byte(`{}`), testTime, nil},
				), nil
			}
			return testutil.NewRows(modeColumns, modeRowValues()), nil
		},
	}

	repo := testRepository(t, db)
	publisher := &fakePublisher{}
	store := outbox.NewRepository(testutil.NewQueries(t, db))
	svc := NewService(repo, WithProducer(publisher), WithEventStore(store))

	if _, err := svc.CreateMode(context.Background(), testUserID.String(), "Motivation", "Be uplifting"); err != nil {
		t.Fatalf("CreateMode() error = %v", err)
	}
	if publisher.calls != 0 {
		t.Errorf("direct publish calls = %d, want 0 when the outbox is used", publisher.calls)
	}
	if enqueued != 2 {
		t.Errorf("outbox inserts = %d, want 2", enqueued)
	}
}

func TestServiceCreateModeValidation(t *testing.T) {
	svc := NewService(nil)

	if _, err := svc.CreateMode(context.Background(), "bad", "Motivation", "Be uplifting"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("got %v, want ErrInvalidInput", err)
	}
}
