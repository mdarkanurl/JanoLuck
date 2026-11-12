package embeddings

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

type fakeEmbedder struct {
	vector  []float64
	err     error
	content string
}

func (f *fakeEmbedder) GenerateEmbeddingContext(_ context.Context, content string) ([]float64, error) {
	f.content = content
	return f.vector, f.err
}

func embeddingPayload(t *testing.T, job Job) []byte {
	t.Helper()
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	return payload
}

func TestJobValidate(t *testing.T) {
	valid := Job{ChatID: uuid.NewString(), Content: "hello"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	for _, job := range []Job{
		{ChatID: "", Content: "hello"},
		{ChatID: "bad", Content: "hello"},
		{ChatID: uuid.NewString(), Content: "   "},
	} {
		if err := job.Validate(); err == nil {
			t.Errorf("Validate(%+v) error = nil, want error", job)
		}
	}
}

func TestVectorString(t *testing.T) {
	tests := []struct {
		in   []float64
		want string
	}{
		{nil, "[]"},
		{[]float64{}, "[]"},
		{[]float64{1.5, -2, 0}, "[1.5,-2,0]"},
	}
	for _, tt := range tests {
		if got := VectorString(tt.in); got != tt.want {
			t.Errorf("VectorString(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWorkerStoresEmbedding(t *testing.T) {
	var stored string
	repo := NewRepository(testutil.NewQueries(t, &testutil.DB{
		Exec: func(_ string, args []driver.NamedValue) (driver.Result, error) {
			stored, _ = args[1].Value.(string)
			return testutil.Result(1)
		},
	}))

	embedder := &fakeEmbedder{vector: []float64{0.1, 0.2}}
	worker := NewWorker(repo, embedder, nil)

	job := Job{ChatID: uuid.NewString(), Content: "hello"}
	if err := worker.Handle(context.Background(), embeddingPayload(t, job)); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if embedder.content != "hello" {
		t.Errorf("embedding content = %q, want hello", embedder.content)
	}
	if stored != "[0.1,0.2]" {
		t.Errorf("stored embedding = %q", stored)
	}
}

func TestWorkerSkipsDeletedChat(t *testing.T) {
	repo := NewRepository(testutil.NewQueries(t, &testutil.DB{
		Exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return testutil.Result(0)
		},
	}))

	worker := NewWorker(repo, &fakeEmbedder{vector: []float64{1}}, nil)
	job := Job{ChatID: uuid.NewString(), Content: "hello"}
	if err := worker.Handle(context.Background(), embeddingPayload(t, job)); err != nil {
		t.Fatalf("Handle() error = %v, want nil for a deleted chat", err)
	}
}

func TestWorkerErrors(t *testing.T) {
	repo := NewRepository(testutil.NewQueries(t, &testutil.DB{}))

	if err := NewWorker(repo, &fakeEmbedder{}, nil).Handle(context.Background(), []byte("{not-json")); err == nil {
		t.Fatal("Handle() error = nil, want decode error")
	}
	if err := NewWorker(repo, &fakeEmbedder{}, nil).Handle(context.Background(), embeddingPayload(t, Job{})); err == nil {
		t.Fatal("Handle() error = nil, want invalid job error")
	}
	if err := NewWorker(repo, nil, nil).Handle(context.Background(), embeddingPayload(t, Job{ChatID: uuid.NewString(), Content: "hello"})); err == nil {
		t.Fatal("Handle() error = nil, want missing embedder error")
	}

	job := Job{ChatID: uuid.NewString(), Content: "hello"}
	if err := NewWorker(repo, &fakeEmbedder{err: errors.New("llm down")}, nil).Handle(context.Background(), embeddingPayload(t, job)); err == nil {
		t.Fatal("Handle() error = nil, want embedder error")
	}
}
