package kafka

import (
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
