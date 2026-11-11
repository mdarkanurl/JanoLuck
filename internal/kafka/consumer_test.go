package kafka

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	ckafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

func TestNewConsumerValidation(t *testing.T) {
	if _, err := NewConsumer(nil, "group", []string{"topic"}); err == nil {
		t.Fatal("NewConsumer with no brokers returned no error")
	}
	if _, err := NewConsumer([]string{"localhost:9092"}, " ", []string{"topic"}); err == nil {
		t.Fatal("NewConsumer with no group returned no error")
	}
	if _, err := NewConsumer([]string{"localhost:9092"}, "group", nil); err == nil {
		t.Fatal("NewConsumer with no topics returned no error")
	}
}

func TestMessageFromKafka(t *testing.T) {
	topic := "email.verification"
	timestamp := time.Now().UTC()

	got := messageFromKafka(&ckafka.Message{
		TopicPartition: ckafka.TopicPartition{
			Topic:     &topic,
			Partition: 2,
			Offset:    42,
		},
		Key:       []byte("user@example.com"),
		Value:     []byte(`{"to":"user@example.com"}`),
		Timestamp: timestamp,
		Headers:   []ckafka.Header{{Key: "trace-id", Value: []byte("abc")}},
	})

	if got.Topic != topic {
		t.Errorf("Topic = %q, want %q", got.Topic, topic)
	}
	if string(got.Key) != "user@example.com" {
		t.Errorf("Key = %q", got.Key)
	}
	if string(got.Value) != `{"to":"user@example.com"}` {
		t.Errorf("Value = %q", got.Value)
	}
	if got.Partition != 2 {
		t.Errorf("Partition = %d, want 2", got.Partition)
	}
	if got.Offset != 42 {
		t.Errorf("Offset = %d, want 42", got.Offset)
	}
	if !got.Timestamp.Equal(timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, timestamp)
	}
	if got.Headers["trace-id"] != "abc" {
		t.Errorf("Headers = %v", got.Headers)
	}
}

type fakePublisher struct {
	calls   int
	topic   string
	key     []byte
	value   []byte
	headers map[string]string
	err     error
}

func (f *fakePublisher) ProduceRaw(_ context.Context, topic string, key, value []byte, headers map[string]string) error {
	f.calls++
	f.topic = topic
	f.key = key
	f.value = value
	f.headers = headers
	return f.err
}

func testConsumer(publisher RawPublisher) *Consumer {
	return &Consumer{
		maxAttempts:      3,
		retryBackoff:     time.Millisecond,
		deadLetter:       publisher,
		deadLetterSuffix: ".dlq",
		logger:           slog.Default(),
	}
}

func TestConsumerHandleRetriesThenSucceeds(t *testing.T) {
	publisher := &fakePublisher{}
	consumer := testConsumer(publisher)

	attempts := 0
	err := consumer.handle(context.Background(), func(context.Context, Message) error {
		attempts++
		if attempts < 3 {
			return errors.New("boom")
		}
		return nil
	}, Message{Topic: "chat.requested", Partition: 1, Offset: 7})

	if err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	if attempts != 3 {
		t.Errorf("handler attempts = %d, want 3", attempts)
	}
	if publisher.calls != 0 {
		t.Errorf("dead-letter calls = %d, want 0", publisher.calls)
	}
}

func TestConsumerHandleDeadLettersAfterExhaustion(t *testing.T) {
	publisher := &fakePublisher{}
	consumer := testConsumer(publisher)

	msg := Message{
		Topic:     "session.title.requested",
		Partition: 2,
		Offset:    42,
		Key:       []byte("session-id"),
		Value:     []byte(`{"content":"hi"}`),
	}

	err := consumer.handle(context.Background(), func(context.Context, Message) error {
		return errors.New("permanent failure")
	}, msg)

	if err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	if publisher.calls != 1 {
		t.Fatalf("dead-letter calls = %d, want 1", publisher.calls)
	}
	if publisher.topic != "session.title.requested.dlq" {
		t.Errorf("dead-letter topic = %q", publisher.topic)
	}
	if string(publisher.key) != "session-id" || string(publisher.value) != `{"content":"hi"}` {
		t.Errorf("dead-letter payload = (%q, %q)", publisher.key, publisher.value)
	}
	if publisher.headers["x-original-topic"] != msg.Topic {
		t.Errorf("x-original-topic = %q", publisher.headers["x-original-topic"])
	}
	if publisher.headers["x-original-partition"] != "2" {
		t.Errorf("x-original-partition = %q", publisher.headers["x-original-partition"])
	}
	if publisher.headers["x-original-offset"] != "42" {
		t.Errorf("x-original-offset = %q", publisher.headers["x-original-offset"])
	}
	if publisher.headers["x-error"] == "" {
		t.Error("x-error header is empty")
	}
}

func TestConsumerHandleReturnsErrorWhenDeadLetterFails(t *testing.T) {
	publisher := &fakePublisher{err: errors.New("broker down")}
	consumer := testConsumer(publisher)

	err := consumer.handle(context.Background(), func(context.Context, Message) error {
		return errors.New("permanent failure")
	}, Message{Topic: "t"})

	if err == nil {
		t.Fatal("handle() returned no error")
	}
}

func TestConsumerHandleDropsWithoutPublisher(t *testing.T) {
	consumer := testConsumer(nil)

	attempts := 0
	err := consumer.handle(context.Background(), func(context.Context, Message) error {
		attempts++
		return errors.New("boom")
	}, Message{Topic: "t"})

	if err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	if attempts != consumer.maxAttempts {
		t.Errorf("handler attempts = %d, want %d", attempts, consumer.maxAttempts)
	}
}

func TestConsumerHandleStopsOnContextCancel(t *testing.T) {
	publisher := &fakePublisher{}
	consumer := testConsumer(publisher)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	attempts := 0
	err := consumer.handle(ctx, func(context.Context, Message) error {
		attempts++
		return errors.New("boom")
	}, Message{Topic: "t"})

	if err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	if attempts != 1 {
		t.Errorf("handler attempts = %d, want 1", attempts)
	}
	if publisher.calls != 0 {
		t.Errorf("dead-letter calls = %d, want 0", publisher.calls)
	}
}
