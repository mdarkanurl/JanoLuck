package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	defaultClientID        = "janoluck"
	defaultDeliveryTimeout = 30 * time.Second
	defaultFlushTimeout    = 10 * time.Second
	schemaVersion          = "1"
)

func jsonHeaders() map[string]string {
	return map[string]string{
		"content_type":   "application/json",
		"schema_version": schemaVersion,
	}
}

var ErrClosed = errors.New("kafka: producer is closed")

type Producer struct {
	client       *kafka.Producer
	deliveryDone chan struct{}
	flushWait    time.Duration
	logger       *slog.Logger

	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

type producerConfig struct {
	clientID        string
	deliveryTimeout time.Duration
	flushTimeout    time.Duration
	logger          *slog.Logger
}

type Option func(*producerConfig)

func WithClientID(clientID string) Option {
	return func(c *producerConfig) {
		if clientID = strings.TrimSpace(clientID); clientID != "" {
			c.clientID = clientID
		}
	}
}

func WithDeliveryTimeout(timeout time.Duration) Option {
	return func(c *producerConfig) {
		if timeout > 0 {
			c.deliveryTimeout = timeout
		}
	}
}

func WithFlushTimeout(timeout time.Duration) Option {
	return func(c *producerConfig) {
		if timeout > 0 {
			c.flushTimeout = timeout
		}
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(c *producerConfig) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func NewProducer(brokers []string, opts ...Option) (*Producer, error) {
	cfg := producerConfig{
		clientID:        defaultClientID,
		deliveryTimeout: defaultDeliveryTimeout,
		flushTimeout:    defaultFlushTimeout,
		logger:          slog.Default(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	brokers = normalizeBrokers(brokers)
	if len(brokers) == 0 {
		return nil, errors.New("kafka: at least one broker is required")
	}

	client, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":       strings.Join(brokers, ","),
		"client.id":               cfg.clientID,
		"acks":                    "all",
		"enable.idempotence":      true,
		"message.timeout.ms":      int(cfg.deliveryTimeout / time.Millisecond),
		"socket.keepalive.enable": true,
	})
	if err != nil {
		return nil, fmt.Errorf("kafka: create producer: %w", err)
	}

	p := &Producer{
		client:       client,
		deliveryDone: make(chan struct{}),
		flushWait:    cfg.flushTimeout,
		logger:       cfg.logger,
	}
	go p.handleDeliveryEvents()

	return p, nil
}

func (p *Producer) Produce(ctx context.Context, topic, key string, value any) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("kafka: topic is required")
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("kafka: marshal message for %q: %w", topic, err)
	}

	return p.produce(ctx, topic, []byte(key), payload, jsonHeaders(), true)
}

func (p *Producer) ProduceAsync(ctx context.Context, topic, key string, value any) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("kafka: topic is required")
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("kafka: marshal message for %q: %w", topic, err)
	}

	return p.produce(ctx, topic, []byte(key), payload, jsonHeaders(), false)
}

func (p *Producer) ProduceRaw(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	return p.produce(ctx, topic, key, value, headers, true)
}

func (p *Producer) ProduceRawAsync(ctx context.Context, topic string, key, value []byte, headers map[string]string) error {
	merged := jsonHeaders()
	for name, headerValue := range headers {
		merged[name] = headerValue
	}

	return p.produce(ctx, topic, key, value, merged, false)
}

func (p *Producer) produce(ctx context.Context, topic string, key, value []byte, headers map[string]string, wait bool) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("kafka: topic is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("kafka: produce to %q: %w", topic, err)
	}

	var delivery chan error
	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &topic,
			Partition: kafka.PartitionAny,
		},
		Key:   key,
		Value: value,
	}
	if wait {
		delivery = make(chan error, 1)
		msg.Opaque = delivery
	}
	if len(headers) > 0 {
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		msg.Headers = make([]kafka.Header, 0, len(headers))
		for _, k := range keys {
			msg.Headers = append(msg.Headers, kafka.Header{Key: k, Value: []byte(headers[k])})
		}
	}

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return ErrClosed
	}
	err := p.client.Produce(msg, nil)
	p.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("kafka: produce to %q: %w", topic, err)
	}
	if !wait {
		return nil
	}

	select {
	case err := <-delivery:
		if err != nil {
			return fmt.Errorf("kafka: deliver to %q: %w", topic, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("kafka: wait for delivery to %q: %w", topic, ctx.Err())
	}
}

func (p *Producer) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		undelivered := p.client.Flush(int(p.flushWait / time.Millisecond))
		p.client.Close()
		p.mu.Unlock()

		<-p.deliveryDone

		if undelivered > 0 {
			p.closeErr = fmt.Errorf("kafka: %d message(s) were not delivered before shutdown", undelivered)
		}
	})

	return p.closeErr
}

func (p *Producer) handleDeliveryEvents() {
	defer close(p.deliveryDone)

	for event := range p.client.Events() {
		switch e := event.(type) {
		case *kafka.Message:
			if ch, ok := e.Opaque.(chan error); ok {
				ch <- e.TopicPartition.Error
				continue
			}
			if e.TopicPartition.Error != nil && p.logger != nil {
				topic := ""
				if e.TopicPartition.Topic != nil {
					topic = *e.TopicPartition.Topic
				}
				p.logger.Error(
					"kafka async delivery failed",
					"topic", topic,
					"partition", e.TopicPartition.Partition,
					"error", e.TopicPartition.Error,
				)
			}
		case kafka.Error:
			if p.logger == nil {
				continue
			}
			if e.IsFatal() {
				p.logger.Error("kafka producer error", "error", e)
			} else {
				p.logger.Warn("kafka producer error", "error", e)
			}
		}
	}
}

func ParseBrokers(raw string) []string {
	return normalizeBrokers(strings.Split(raw, ","))
}

func normalizeBrokers(brokers []string) []string {
	normalized := make([]string, 0, len(brokers))
	seen := make(map[string]struct{}, len(brokers))

	for _, broker := range brokers {
		broker = strings.TrimSpace(broker)
		if broker == "" {
			continue
		}
		if _, ok := seen[broker]; ok {
			continue
		}
		seen[broker] = struct{}{}
		normalized = append(normalized, broker)
	}

	return normalized
}
