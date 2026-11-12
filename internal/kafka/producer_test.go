package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseBrokers(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: []string{}},
		{name: "single", raw: "localhost:9092", want: []string{"localhost:9092"}},
		{name: "multiple", raw: "localhost:9092,localhost:9093", want: []string{"localhost:9092", "localhost:9093"}},
		{name: "trims and skips empties", raw: " localhost:9092 , ,kafka:9092 ,", want: []string{"localhost:9092", "kafka:9092"}},
		{name: "deduplicates", raw: "localhost:9092, localhost:9092", want: []string{"localhost:9092"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseBrokers(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseBrokers(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ParseBrokers(%q) = %v, want %v", tt.raw, got, tt.want)
				}
			}
		})
	}
}

func TestNewProducerRequiresBroker(t *testing.T) {
	if _, err := NewProducer(nil); err == nil {
		t.Fatal("NewProducer(nil) returned no error")
	}

	if _, err := NewProducer([]string{"  ", ""}); err == nil {
		t.Fatal("NewProducer with only blank brokers returned no error")
	}
}

func TestProduceRejectsInvalidInput(t *testing.T) {
	producer, err := NewProducer([]string{"127.0.0.1:9092"})
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})

	if err := producer.Produce(context.Background(), " ", "key", map[string]string{"a": "b"}); err == nil {
		t.Fatal("Produce with empty topic returned no error")
	}

	if err := producer.Produce(context.Background(), "events", "key", make(chan int)); err == nil {
		t.Fatal("Produce with unmarshalable value returned no error")
	}
}

func TestProduceRawAsyncRejectsInvalidInput(t *testing.T) {
	producer, err := NewProducer([]string{"127.0.0.1:9092"})
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})

	if err := producer.ProduceRawAsync(context.Background(), " ", []byte("key"), []byte(`{}`), nil); err == nil {
		t.Fatal("ProduceRawAsync with empty topic returned no error")
	}

	if err := producer.ProduceRawAsync(context.Background(), "events", []byte("key"), []byte(`{}`), nil); err != nil {
		t.Fatalf("ProduceRawAsync: %v", err)
	}
}

func TestProduceAfterClose(t *testing.T) {
	producer, err := NewProducer(
		[]string{"127.0.0.1:9092"},
		WithDeliveryTimeout(100*time.Millisecond),
		WithFlushTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = producer.Produce(context.Background(), "events", "key", map[string]string{"a": "b"})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Produce after Close = %v, want %v", err, ErrClosed)
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
