package sessions

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

type fakeSummaryGenerator struct {
	summary  string
	err      error
	calls    int
	contents []string
}

func (f *fakeSummaryGenerator) SummarizeSessionContext(_ context.Context, contents []string) (string, error) {
	f.calls++
	f.contents = contents
	return f.summary, f.err
}

func summaryPayload(t *testing.T, job SummaryJob) []byte {
	t.Helper()
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	return payload
}

func validSummaryJob() SummaryJob {
	return SummaryJob{SessionID: sessionID.String(), UserID: sessionUserID.String()}
}

func summaryRepo(t *testing.T, count int64, existing *string, exec func([]driver.NamedValue) (driver.Result, error)) *Repository {
	t.Helper()

	return testRepository(t, &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "CountChatsBySessionID"):
				return testutil.NewRows([]string{"count"}, []driver.Value{count}), nil
			case strings.Contains(query, "GetSessionSummary"):
				if existing == nil {
					return testutil.QueryError(sql.ErrNoRows)
				}
				return testutil.NewRows([]string{"summary", "coveredChats"}, []driver.Value{*existing, int32(count)}), nil
			case strings.Contains(query, "ListRecentChatContentsBySessionID"):
				return testutil.NewRows([]string{"content"}, []driver.Value{"first"}, []driver.Value{"second"}), nil
			}
			return testutil.QueryError(errors.New("unexpected query"))
		},
		Exec: func(_ string, args []driver.NamedValue) (driver.Result, error) {
			return exec(args)
		},
	})
}

func TestSummaryWorkerStoresSummary(t *testing.T) {
	var storedSummary string
	var storedCovered int64
	repo := summaryRepo(t, 10, nil, func(args []driver.NamedValue) (driver.Result, error) {
		storedSummary, _ = args[1].Value.(string)
		storedCovered, _ = args[2].Value.(int64)
		return testutil.Result(1)
	})

	generator := &fakeSummaryGenerator{summary: "  The story so far  "}
	worker := NewSummaryWorker(repo, generator, WithSummaryWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), summaryPayload(t, validSummaryJob())); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if generator.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", generator.calls)
	}
	if len(generator.contents) != 2 || generator.contents[0] != "first" {
		t.Errorf("generator contents = %v", generator.contents)
	}
	if storedSummary != "The story so far" {
		t.Errorf("stored summary = %q", storedSummary)
	}
	if storedCovered != 10 {
		t.Errorf("stored covered chats = %d, want 10", storedCovered)
	}
}

func TestSummaryWorkerSkipsNonBatchBoundary(t *testing.T) {
	repo := summaryRepo(t, 7, nil, func([]driver.NamedValue) (driver.Result, error) {
		t.Fatal("unexpected upsert")
		return testutil.Result(1)
	})

	generator := &fakeSummaryGenerator{summary: "unused"}
	worker := NewSummaryWorker(repo, generator, WithSummaryWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), summaryPayload(t, validSummaryJob())); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if generator.calls != 0 {
		t.Errorf("generator calls = %d, want 0", generator.calls)
	}
}

func TestSummaryWorkerSkipsAlreadyCovered(t *testing.T) {
	existing := "already summarized"
	repo := summaryRepo(t, 10, &existing, func([]driver.NamedValue) (driver.Result, error) {
		t.Fatal("unexpected upsert")
		return testutil.Result(1)
	})

	generator := &fakeSummaryGenerator{summary: "unused"}
	worker := NewSummaryWorker(repo, generator, WithSummaryWorkerLogger(discardLogger()))

	if err := worker.Handle(context.Background(), summaryPayload(t, validSummaryJob())); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if generator.calls != 0 {
		t.Errorf("generator calls = %d, want 0", generator.calls)
	}
}

func TestSummaryWorkerErrors(t *testing.T) {
	repo := summaryRepo(t, 10, nil, func([]driver.NamedValue) (driver.Result, error) {
		return testutil.Result(1)
	})

	if err := NewSummaryWorker(repo, &fakeSummaryGenerator{}, WithSummaryWorkerLogger(discardLogger())).Handle(context.Background(), []byte("{not-json")); !errors.Is(err, ErrInvalidSummaryJob) {
		t.Errorf("Handle() error = %v, want ErrInvalidSummaryJob", err)
	}
	if err := NewSummaryWorker(repo, &fakeSummaryGenerator{}, WithSummaryWorkerLogger(discardLogger())).Handle(context.Background(), summaryPayload(t, SummaryJob{})); !errors.Is(err, ErrInvalidSummaryJob) {
		t.Errorf("Handle() error = %v, want ErrInvalidSummaryJob", err)
	}
	if err := NewSummaryWorker(repo, &fakeSummaryGenerator{summary: "   "}, WithSummaryWorkerLogger(discardLogger())).Handle(context.Background(), summaryPayload(t, validSummaryJob())); err == nil {
		t.Fatal("Handle() error = nil, want empty summary error")
	}
	if err := NewSummaryWorker(repo, &fakeSummaryGenerator{err: errors.New("llm down")}, WithSummaryWorkerLogger(discardLogger())).Handle(context.Background(), summaryPayload(t, validSummaryJob())); err == nil {
		t.Fatal("Handle() error = nil, want generator error")
	}
}
