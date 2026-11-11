package email

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeSender struct {
	errs  []error
	calls int
	to    []string
}

func (f *fakeSender) Send(to, subject, body string) error {
	f.calls++
	f.to = append(f.to, to)
	if f.calls <= len(f.errs) {
		return f.errs[f.calls-1]
	}
	return nil
}

func marshalJob(t *testing.T, job Job) []byte {
	t.Helper()
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	return payload
}

func validJob() Job {
	return Job{
		To:      "user@example.com",
		Subject: "verify your e-mail",
		Body:    "here is your verification code:123456",
	}
}

func TestWorkerHandleSendsEmail(t *testing.T) {
	sender := &fakeSender{}
	worker := NewWorker(sender)

	if err := worker.Handle(context.Background(), VerificationTopic, marshalJob(t, validJob()), time.Now()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if sender.calls != 1 {
		t.Fatalf("Send calls = %d, want 1", sender.calls)
	}
	if sender.to[0] != "user@example.com" {
		t.Errorf("Send to = %q, want user@example.com", sender.to[0])
	}
}

func TestWorkerHandleInvalidPayload(t *testing.T) {
	sender := &fakeSender{}
	worker := NewWorker(sender)

	err := worker.Handle(context.Background(), VerificationTopic, []byte("{not-json"), time.Now())
	if !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("Handle() error = %v, want ErrInvalidJob", err)
	}

	if sender.calls != 0 {
		t.Fatalf("Send calls = %d, want 0", sender.calls)
	}
}

func TestWorkerHandleInvalidJob(t *testing.T) {
	sender := &fakeSender{}
	worker := NewWorker(sender)

	job := validJob()
	job.To = "not-an-email"

	err := worker.Handle(context.Background(), VerificationTopic, marshalJob(t, job), time.Now())
	if !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("Handle() error = %v, want ErrInvalidJob", err)
	}
}

func TestWorkerHandleRetriesThenSucceeds(t *testing.T) {
	sender := &fakeSender{errs: []error{errors.New("boom"), errors.New("boom")}}
	worker := NewWorker(
		sender,
		WithMaxAttempts(3),
		WithRetryDelay(time.Millisecond),
	)

	if err := worker.Handle(context.Background(), VerificationTopic, marshalJob(t, validJob()), time.Now()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if sender.calls != 3 {
		t.Fatalf("Send calls = %d, want 3", sender.calls)
	}
}

func TestWorkerHandleExhaustsRetries(t *testing.T) {
	sender := &fakeSender{errs: []error{
		errors.New("boom"),
		errors.New("boom"),
		errors.New("boom"),
	}}
	worker := NewWorker(
		sender,
		WithMaxAttempts(3),
		WithRetryDelay(time.Millisecond),
	)

	err := worker.Handle(context.Background(), VerificationTopic, marshalJob(t, validJob()), time.Now())
	if err == nil {
		t.Fatal("Handle() returned no error")
	}

	if sender.calls != 3 {
		t.Fatalf("Send calls = %d, want 3", sender.calls)
	}
}

func TestWorkerHandleSkipsStaleJob(t *testing.T) {
	sender := &fakeSender{}
	worker := NewWorker(sender, WithMaxAge(5*time.Minute))

	err := worker.Handle(
		context.Background(),
		VerificationTopic,
		marshalJob(t, validJob()),
		time.Now().Add(-10*time.Minute),
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if sender.calls != 0 {
		t.Fatalf("Send calls = %d, want 0", sender.calls)
	}
}

func TestWorkerHandleUsesPasswordResetMaxAge(t *testing.T) {
	sender := &fakeSender{}
	worker := NewWorker(sender)

	err := worker.Handle(
		context.Background(),
		PasswordResetTopic,
		marshalJob(t, validJob()),
		time.Now().Add(-6*time.Minute),
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if sender.calls != 1 {
		t.Fatalf("Send calls = %d, want 1", sender.calls)
	}
}

func TestWorkerHandleStopsOnContextCancel(t *testing.T) {
	sender := &fakeSender{errs: []error{errors.New("boom")}}
	worker := NewWorker(
		sender,
		WithMaxAttempts(5),
		WithRetryDelay(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := worker.Handle(ctx, VerificationTopic, marshalJob(t, validJob()), time.Now())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Handle() error = %v, want context.Canceled", err)
	}
}
