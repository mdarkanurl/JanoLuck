package chats

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

var outboxColumns = []string{"id", "topic", "key", "payload", "headers", "create_at", "published_at"}

func outboxRow() []driver.Value {
	return []driver.Value{int64(1), "topic", "key", []byte(`{}`), []byte(`{}`), testTime, nil}
}

// exchangeDB answers the queries of the chat creation flow and records the
// contents passed to every CreateChat insert.
func exchangeDB(contents *[]string, enqueued *int) *testutil.DB {
	return &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "SessionExistsByUserID"):
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			case strings.Contains(query, "GetActiveModeByUserID"):
				return testutil.QueryError(sql.ErrNoRows)
			case strings.Contains(query, "ListSimilarChatsBySessionID"):
				return testutil.NewRows([]string{"content"}, []driver.Value{"an older message"}), nil
			case strings.Contains(query, "GetSessionSummary"):
				return testutil.QueryError(sql.ErrNoRows)
			case strings.Contains(query, "CreateChat"):
				values := chatRowValues()
				if len(args) > 3 {
					if role, ok := args[2].Value.(sql.NullString); ok {
						values[3] = role.String
					}
					values[4] = fmt.Sprint(args[3].Value)
					if contents != nil {
						*contents = append(*contents, fmt.Sprint(args[3].Value))
					}
				}
				return testutil.NewRows(chatColumns, values), nil
			case strings.Contains(query, "EnqueueOutbox"):
				if enqueued != nil {
					(*enqueued)++
				}
				return testutil.NewRows(outboxColumns, outboxRow()), nil
			case strings.Contains(query, "CountChatsBySessionID"):
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(2)}), nil
			default:
				return testutil.QueryError(fmt.Errorf("unexpected query: %s", query))
			}
		},
	}
}

func TestServiceCreateChatPersistsUserMessageAndReplyTogether(t *testing.T) {
	var contents []string
	var enqueued int

	db := exchangeDB(&contents, &enqueued)
	repo := testRepository(t, db)
	store := outbox.NewRepository(testutil.NewQueries(t, db))

	model := &fakeLanguageModel{prediction: "A bright future awaits"}
	svc := NewService(repo, model, WithEventStore(store))

	reply, err := svc.CreateChat(context.Background(), testUserID, testSessionID, "What next?")
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	if reply.Content != "A bright future awaits" {
		t.Errorf("reply = %+v, want the stored prediction", reply)
	}

	if len(contents) != 2 {
		t.Fatalf("CreateChat inserts = %d, want 2 (%v)", len(contents), contents)
	}
	if contents[0] != "What next?" {
		t.Errorf("first insert = %q, want the user message", contents[0])
	}
	if contents[1] != "A bright future awaits" {
		t.Errorf("second insert = %q, want the prediction", contents[1])
	}

	// moderation + analytics for the user message, analytics for the reply.
	if enqueued != 3 {
		t.Errorf("outbox inserts = %d, want 3", enqueued)
	}
}

func TestServiceCreateChatDoesNotPersistWhenPredictionFails(t *testing.T) {
	contents := []string{}
	db := exchangeDB(&contents, nil)

	model := &fakeLanguageModel{predictionErr: errors.New("model down")}
	svc := NewService(testRepository(t, db), model, WithEventStore(outbox.NewRepository(testutil.NewQueries(t, db))))

	_, err := svc.CreateChat(context.Background(), testUserID, testSessionID, "What next?")
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("error = %v, want ErrInternal", err)
	}
	if len(contents) != 0 {
		t.Fatalf("inserted %d chats after a failed prediction (%v), want 0", len(contents), contents)
	}
}

func TestServiceCreateChatDoesNotPersistWhenEmbeddingFails(t *testing.T) {
	contents := []string{}
	db := exchangeDB(&contents, nil)

	model := &fakeLanguageModel{prediction: "A bright future awaits", embeddingErr: errors.New("embedding down"), embeddingErrAt: 2}
	svc := NewService(testRepository(t, db), model, WithEventStore(outbox.NewRepository(testutil.NewQueries(t, db))))

	_, err := svc.CreateChat(context.Background(), testUserID, testSessionID, "What next?")
	if !errors.Is(err, ErrInternal) {
		t.Fatalf("error = %v, want ErrInternal", err)
	}
	if len(contents) != 0 {
		t.Fatalf("inserted %d chats after a failed embedding (%v), want 0", len(contents), contents)
	}
}

func TestRepositoryCreateChatsWithEvents(t *testing.T) {
	contents := []string{}
	db := exchangeDB(&contents, nil)

	repo := testRepository(t, db)
	messages := []NewChat{
		{Role: RoleUser, Content: "question", Embedding: "[0.1]"},
		{Role: RoleSystem, Content: "answer", Embedding: "[0.2]"},
	}

	var events []int
	chats, err := repo.CreateChatsWithEvents(context.Background(), testSessionID, testUserID, messages, func(index int, _ Chat) []outbox.Event {
		events = append(events, index)
		return nil
	})
	if err != nil {
		t.Fatalf("CreateChatsWithEvents() error = %v", err)
	}
	if len(chats) != 2 {
		t.Fatalf("chats = %d, want 2", len(chats))
	}
	if len(contents) != 2 || contents[0] != "question" || contents[1] != "answer" {
		t.Errorf("stored contents = %v", contents)
	}
	if len(events) != 2 || events[0] != 0 || events[1] != 1 {
		t.Errorf("build indexes = %v, want [0 1]", events)
	}
}
