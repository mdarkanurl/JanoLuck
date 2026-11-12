package sessions

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

type fakeGenerator struct {
	name    string
	err     error
	calls   int
	content string
}

func (f *fakeGenerator) GenerateSessionNameContext(_ context.Context, content string) (string, error) {
	f.calls++
	f.content = content
	return f.name, f.err
}

type fakePublisher struct {
	calls int
	topic string
	key   string
	value any
	err   error
}

func (f *fakePublisher) Produce(_ context.Context, topic, key string, value any) error {
	f.calls++
	f.topic = topic
	f.key = key
	f.value = value
	return f.err
}

func (f *fakePublisher) ProduceAsync(_ context.Context, topic, key string, value any) error {
	f.calls++
	f.topic = topic
	f.key = key
	f.value = value
	return f.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFallbackSessionName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "New session"},
		{name: "whitespace", content: "   \n\t ", want: "New session"},
		{name: "collapses whitespace", content: "will I   find\nlove?", want: "will I find love?"},
		{name: "truncates", content: strings.Repeat("a", 80), want: strings.Repeat("a", 60) + "…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallbackSessionName(tt.content); got != tt.want {
				t.Errorf("fallbackSessionName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceCreateSessionPublishesTitleJob(t *testing.T) {
	var storedName string
	repo := testRepository(t, &testutil.DB{
		Query: func(_ string, args []driver.NamedValue) (driver.Rows, error) {
			storedName, _ = args[0].Value.(string)
			return testutil.NewRows(sessionColumns, sessionRowValues()), nil
		},
	})

	generator := &fakeGenerator{name: "Generated Title"}
	publisher := &fakePublisher{}
	svc := NewService(repo, generator, WithProducer(publisher))

	session, err := svc.CreateSession(context.Background(), sessionUserID.String(), "Will I find love?")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.ID != sessionID {
		t.Errorf("session ID = %s, want %s", session.ID, sessionID)
	}
	if storedName != "Will I find love?" {
		t.Errorf("stored name = %q, want the fallback name", storedName)
	}
	if generator.calls != 0 {
		t.Errorf("generator calls = %d, want 0 for the async path", generator.calls)
	}

	if publisher.calls != 1 {
		t.Fatalf("publish calls = %d, want 1", publisher.calls)
	}
	if publisher.topic != TitleTopic || publisher.key != sessionID.String() {
		t.Errorf("published to (%q, %q), want (%q, %s)", publisher.topic, publisher.key, TitleTopic, sessionID)
	}

	job, ok := publisher.value.(TitleJob)
	if !ok {
		t.Fatalf("published value type = %T, want TitleJob", publisher.value)
	}
	if job.SessionID != sessionID.String() || job.UserID != sessionUserID.String() {
		t.Errorf("job IDs = (%q, %q)", job.SessionID, job.UserID)
	}
	if job.Content != "Will I find love?" {
		t.Errorf("job content = %q", job.Content)
	}
}

func TestServiceCreateSessionSucceedsWhenPublishFails(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.NewRows(sessionColumns, sessionRowValues()), nil
		},
	})

	svc := NewService(
		repo,
		&fakeGenerator{},
		WithProducer(&fakePublisher{err: errors.New("kafka down")}),
		WithLogger(discardLogger()),
	)

	if _, err := svc.CreateSession(context.Background(), sessionUserID.String(), "hello"); err != nil {
		t.Fatalf("CreateSession() error = %v, want nil", err)
	}
}

func TestServiceCreateSessionGeneratesInlineWithoutProducer(t *testing.T) {
	var storedName string
	repo := testRepository(t, &testutil.DB{
		Query: func(_ string, args []driver.NamedValue) (driver.Rows, error) {
			storedName, _ = args[0].Value.(string)
			return testutil.NewRows(sessionColumns, sessionRowValues()), nil
		},
	})

	generator := &fakeGenerator{name: "  Inline Title  "}
	svc := NewService(repo, generator, WithLogger(discardLogger()))

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

func TestServiceCreateSessionKeepsFallbackWhenInlineGenerationFails(t *testing.T) {
	var storedName string
	repo := testRepository(t, &testutil.DB{
		Query: func(_ string, args []driver.NamedValue) (driver.Rows, error) {
			storedName, _ = args[0].Value.(string)
			return testutil.NewRows(sessionColumns, sessionRowValues()), nil
		},
	})

	svc := NewService(
		repo,
		&fakeGenerator{err: errors.New("llm down")},
		WithLogger(discardLogger()),
	)

	if _, err := svc.CreateSession(context.Background(), sessionUserID.String(), "hello"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if storedName != "hello" {
		t.Errorf("stored name = %q, want fallback %q", storedName, "hello")
	}
}

func titlePayload(t *testing.T, job TitleJob) []byte {
	t.Helper()
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	return payload
}

func validTitleJob() TitleJob {
	return TitleJob{
		SessionID: sessionID.String(),
		UserID:    sessionUserID.String(),
		Content:   "Will I find love?",
	}
}

func TestTitleWorkerHandleUpdatesSessionName(t *testing.T) {
	var storedName string
	repo := testRepository(t, &testutil.DB{
		Exec: func(_ string, args []driver.NamedValue) (driver.Result, error) {
			storedName, _ = args[2].Value.(string)
			return testutil.Result(1)
		},
	})

	generator := &fakeGenerator{name: "  The Heart's Hidden Roadmap  "}
	worker := NewTitleWorker(repo, generator, WithTitleWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), titlePayload(t, validTitleJob()), time.Now()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if generator.content != "Will I find love?" {
		t.Errorf("generator content = %q", generator.content)
	}
	if storedName != "The Heart's Hidden Roadmap" {
		t.Errorf("stored name = %q", storedName)
	}
}

func TestTitleWorkerHandleInvalidPayload(t *testing.T) {
	worker := NewTitleWorker(nil, &fakeGenerator{}, WithTitleWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), []byte("{not-json"), time.Now()); !errors.Is(err, ErrInvalidTitleJob) {
		t.Errorf("Handle() error = %v, want ErrInvalidTitleJob", err)
	}
}

func TestTitleWorkerHandleInvalidJob(t *testing.T) {
	worker := NewTitleWorker(nil, &fakeGenerator{}, WithTitleWorkerLogger(discardLogger()))

	job := validTitleJob()
	job.UserID = ""
	if err := worker.Handle(context.Background(), titlePayload(t, job), time.Now()); !errors.Is(err, ErrInvalidTitleJob) {
		t.Errorf("Handle() error = %v, want ErrInvalidTitleJob", err)
	}
}

func TestTitleWorkerHandleSkipsStaleJob(t *testing.T) {
	repo := testRepository(t, &testutil.DB{})
	generator := &fakeGenerator{name: "unused"}
	worker := NewTitleWorker(repo, generator, WithTitleWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), titlePayload(t, validTitleJob()), time.Now().Add(-11*time.Minute)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if generator.calls != 0 {
		t.Errorf("generator calls = %d, want 0", generator.calls)
	}
}

func TestTitleWorkerHandleSessionNotFound(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return testutil.Result(0)
		},
	})
	worker := NewTitleWorker(repo, &fakeGenerator{name: "Title"}, WithTitleWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), titlePayload(t, validTitleJob()), time.Now()); err != nil {
		t.Fatalf("Handle() error = %v, want nil for a deleted session", err)
	}
}

func TestTitleWorkerHandleGeneratorError(t *testing.T) {
	repo := testRepository(t, &testutil.DB{})
	worker := NewTitleWorker(
		repo,
		&fakeGenerator{err: errors.New("llm down")},
		WithTitleWorkerLogger(discardLogger()),
	)

	if err := worker.Handle(context.Background(), titlePayload(t, validTitleJob()), time.Now()); err == nil {
		t.Fatal("Handle() returned no error")
	}
}

func TestTitleWorkerHandleEmptyName(t *testing.T) {
	repo := testRepository(t, &testutil.DB{})
	worker := NewTitleWorker(repo, &fakeGenerator{name: "   "}, WithTitleWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), titlePayload(t, validTitleJob()), time.Now()); err == nil {
		t.Fatal("Handle() returned no error")
	}
}

func TestTitleWorkerHandleRepositoryError(t *testing.T) {
	repo := testRepository(t, &testutil.DB{
		Exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return testutil.ExecError(errors.New("boom"))
		},
	})
	worker := NewTitleWorker(repo, &fakeGenerator{name: "Title"}, WithTitleWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), titlePayload(t, validTitleJob()), time.Now()); !errors.Is(err, ErrInternal) {
		t.Errorf("Handle() error = %v, want ErrInternal", err)
	}
}

func TestTitleWorkerHandleTruncatesLongName(t *testing.T) {
	var storedName string
	repo := testRepository(t, &testutil.DB{
		Exec: func(_ string, args []driver.NamedValue) (driver.Result, error) {
			storedName, _ = args[2].Value.(string)
			return testutil.Result(1)
		},
	})

	worker := NewTitleWorker(
		repo,
		&fakeGenerator{name: strings.Repeat("a", 200)},
		WithTitleWorkerLogger(discardLogger()),
	)

	if err := worker.Handle(context.Background(), titlePayload(t, validTitleJob()), time.Now()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len([]rune(storedName)) != maxSessionNameLength {
		t.Errorf("stored name length = %d, want %d", len([]rune(storedName)), maxSessionNameLength)
	}
}
