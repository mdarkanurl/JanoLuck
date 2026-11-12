package chats

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func TestRepositoryCreateChatWithEvents(t *testing.T) {
	var enqueued []string
	repo := testRepository(t, &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "EnqueueOutbox") {
				enqueued = append(enqueued, query)
				return testutil.NewRows(
					[]string{"id", "topic", "key", "payload", "headers", "create_at", "published_at"},
					[]driver.Value{int64(1), "moderation.requested", "key", []byte(`{}`), []byte(`{}`), testTime, nil},
				), nil
			}
			return testutil.NewRows(chatColumns, chatRowValues()), nil
		},
	})

	chat, err := repo.CreateChatWithEvents(context.Background(), testSessionID, testUserID, RoleUser, "hello", "[]", func(Chat) []outbox.Event {
		return []outbox.Event{{Topic: "moderation.requested", Key: "key", Payload: map[string]string{"id": "1"}}}
	})
	if err != nil {
		t.Fatalf("CreateChatWithEvents() error = %v", err)
	}
	if chat.ID != testChatID {
		t.Errorf("chat id = %s, want %s", chat.ID, testChatID)
	}
	if len(enqueued) != 1 {
		t.Errorf("outbox inserts = %d, want 1", len(enqueued))
	}
}

func TestRepositoryCreateChatWithEventsRollsBackOnFailure(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "EnqueueOutbox") {
				return testutil.QueryError(errors.New("boom"))
			}
			return testutil.NewRows(chatColumns, chatRowValues()), nil
		},
	})

	_, err := repo.CreateChatWithEvents(context.Background(), testSessionID, testUserID, RoleUser, "hello", "[]", func(Chat) []outbox.Event {
		return []outbox.Event{{Topic: "moderation.requested", Key: "key", Payload: map[string]string{"id": "1"}}}
	})
	if !errors.Is(err, ErrInternal) {
		t.Errorf("got %v, want ErrInternal", err)
	}
}
