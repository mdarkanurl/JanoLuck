package sessions

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func TestServiceCreateSessionEnqueuesTitleThroughOutbox(t *testing.T) {
	var enqueued int
	db := &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "EnqueueOutbox") {
				enqueued++
				return testutil.NewRows(
					[]string{"id", "topic", "key", "payload", "headers", "create_at", "published_at"},
					[]driver.Value{int64(1), TitleTopic, "key", []byte(`{}`), []byte(`{}`), sessionTime, nil},
				), nil
			}
			return testutil.NewRows(sessionColumns, sessionRowValues()), nil
		},
	}

	repo := testRepository(t, db)
	publisher := &fakePublisher{}
	store := outbox.NewRepository(testutil.NewQueries(t, db))
	svc := NewService(repo, &fakeGenerator{name: "unused"}, WithProducer(publisher), WithEventStore(store))

	if _, err := svc.CreateSession(context.Background(), sessionUserID.String(), "hello"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if publisher.calls != 0 {
		t.Errorf("direct publish calls = %d, want 0 when the outbox is used", publisher.calls)
	}
	if enqueued != 1 {
		t.Errorf("outbox inserts = %d, want 1", enqueued)
	}
}

func TestServiceCreateSessionGeneratesInlineWithoutProducerOrStore(t *testing.T) {
	var storedName string
	repo := testRepository(t, &testutil.DB{
		Query: func(_ string, args []driver.NamedValue) (driver.Rows, error) {
			storedName, _ = args[0].Value.(string)
			return testutil.NewRows(sessionColumns, sessionRowValues()), nil
		},
	})

	generator := &fakeGenerator{name: "Inline Title"}
	svc := NewService(repo, generator)

	if _, err := svc.CreateSession(context.Background(), sessionUserID.String(), "hello"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if generator.calls != 1 {
		t.Errorf("generator calls = %d, want 1", generator.calls)
	}
	if storedName != "Inline Title" {
		t.Errorf("stored name = %q, want %q", storedName, "Inline Title")
	}
}

func TestServiceCreateSessionFallsBackWhenOutboxFails(t *testing.T) {
	db := &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "EnqueueOutbox") {
				return testutil.QueryError(errOutbox)
			}
			return testutil.NewRows(sessionColumns, sessionRowValues()), nil
		},
	}

	repo := testRepository(t, db)
	store := outbox.NewRepository(testutil.NewQueries(t, db))
	svc := NewService(repo, &fakeGenerator{name: "unused"}, WithEventStore(store), WithLogger(discardLogger()))

	if _, err := svc.CreateSession(context.Background(), sessionUserID.String(), "hello"); err != nil {
		t.Fatalf("CreateSession() error = %v, want nil", err)
	}
}

var errOutbox = errors.New("outbox unavailable")
