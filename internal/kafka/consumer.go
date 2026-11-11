package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	defaultPollTimeout      = time.Second
	defaultConsumerID       = "janoluck-worker"
	defaultMaxAttempts      = 3
	defaultRetryBackoff     = time.Second
	maxRetryBackoff         = 30 * time.Second
	defaultDeadLetterSuffix = ".dlq"
	deadLetterTimeout       = 10 * time.Second
	maxErrorHeaderLength    = 900
)

type Message struct {
	Topic     string
	Key       []byte
	Value     []byte
	Headers   map[string]string
	Partition int32
	Offset    int64
	Timestamp time.Time
}

type Handler func(ctx context.Context, msg Message) error

type RawPublisher interface {
	ProduceRaw(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
}

type Consumer struct {
	client      *kafka.Consumer
	pollTimeout time.Duration
	logger      *slog.Logger

	maxAttempts      int
	retryBackoff     time.Duration
	deadLetter       RawPublisher
	deadLetterSuffix string

	closeOnce sync.Once
	closeErr  error
}

type consumerConfig struct {
	clientID         string
	pollTimeout      time.Duration
	logger           *slog.Logger
	maxAttempts      int
	retryBackoff     time.Duration
	deadLetter       RawPublisher
	deadLetterSuffix string
}

type ConsumerOption func(*consumerConfig)

func WithConsumerClientID(clientID string) ConsumerOption {
	return func(c *consumerConfig) {
		if clientID = strings.TrimSpace(clientID); clientID != "" {
			c.clientID = clientID
		}
	}
}

func WithPollTimeout(timeout time.Duration) ConsumerOption {
	return func(c *consumerConfig) {
		if timeout > 0 {
			c.pollTimeout = timeout
		}
	}
}

func WithConsumerLogger(logger *slog.Logger) ConsumerOption {
	return func(c *consumerConfig) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func WithMaxAttempts(attempts int) ConsumerOption {
	return func(c *consumerConfig) {
		if attempts > 0 {
			c.maxAttempts = attempts
		}
	}
}

func WithRetryBackoff(backoff time.Duration) ConsumerOption {
	return func(c *consumerConfig) {
		if backoff > 0 {
			c.retryBackoff = backoff
		}
	}
}

func WithDeadLetterPublisher(publisher RawPublisher) ConsumerOption {
	return func(c *consumerConfig) {
		if publisher != nil {
			c.deadLetter = publisher
		}
	}
}

func WithDeadLetterTopicSuffix(suffix string) ConsumerOption {
	return func(c *consumerConfig) {
		if suffix != "" {
			c.deadLetterSuffix = suffix
		}
	}
}

func NewConsumer(brokers []string, groupID string, topics []string, opts ...ConsumerOption) (*Consumer, error) {
	cfg := consumerConfig{
		clientID:         defaultConsumerID,
		pollTimeout:      defaultPollTimeout,
		logger:           slog.Default(),
		maxAttempts:      defaultMaxAttempts,
		retryBackoff:     defaultRetryBackoff,
		deadLetterSuffix: defaultDeadLetterSuffix,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	brokers = normalizeBrokers(brokers)
	if len(brokers) == 0 {
		return nil, errors.New("kafka: at least one broker is required")
	}

	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, errors.New("kafka: consumer group is required")
	}

	topics = normalizeBrokers(topics)
	if len(topics) == 0 {
		return nil, errors.New("kafka: at least one topic is required")
	}

	client, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":        strings.Join(brokers, ","),
		"client.id":                cfg.clientID,
		"group.id":                 groupID,
		"auto.offset.reset":        "earliest",
		"enable.auto.commit":       false,
		"enable.auto.offset.store": false,
		"allow.auto.create.topics": true,
		"socket.keepalive.enable":  true,
	})
	if err != nil {
		return nil, fmt.Errorf("kafka: create consumer: %w", err)
	}

	if err := client.SubscribeTopics(topics, nil); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("kafka: subscribe: %w", err)
	}

	return &Consumer{
		client:           client,
		pollTimeout:      cfg.pollTimeout,
		logger:           cfg.logger,
		maxAttempts:      cfg.maxAttempts,
		retryBackoff:     cfg.retryBackoff,
		deadLetter:       cfg.deadLetter,
		deadLetterSuffix: cfg.deadLetterSuffix,
	}, nil
}

func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		msg, err := c.client.ReadMessage(c.pollTimeout)
		if err != nil {
			var kafkaErr kafka.Error
			if errors.As(err, &kafkaErr) {
				if kafkaErr.IsTimeout() || kafkaErr.Code() == kafka.ErrPartitionEOF {
					continue
				}
				if kafkaErr.IsFatal() {
					return fmt.Errorf("kafka: consume: %w", err)
				}
			}

			c.logger.Warn("kafka consume error", "error", err)
			continue
		}

		m := messageFromKafka(msg)
		if err := c.handle(ctx, handler, m); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}

		if _, err := c.client.CommitMessage(msg); err != nil {
			c.logger.Error(
				"kafka commit failed",
				"topic", m.Topic,
				"partition", m.Partition,
				"offset", m.Offset,
				"error", err,
			)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, handler Handler, msg Message) error {
	var lastErr error

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		lastErr = handler(ctx, msg)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}

		c.logger.Error(
			"kafka message handler failed",
			"topic", msg.Topic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"attempt", attempt,
			"max_attempts", c.maxAttempts,
			"error", lastErr,
		)

		if attempt == c.maxAttempts {
			break
		}

		delay := c.retryDelayFor(attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}

	if c.deadLetter == nil {
		c.logger.Error(
			"kafka message dropped after exhausting retries; no dead-letter publisher configured",
			"topic", msg.Topic,
			"partition", msg.Partition,
			"offset", msg.Offset,
			"error", lastErr,
		)
		return nil
	}

	dlqTopic := msg.Topic + c.deadLetterSuffix
	headers := map[string]string{
		"x-original-topic":     msg.Topic,
		"x-original-partition": strconv.Itoa(int(msg.Partition)),
		"x-original-offset":    strconv.FormatInt(msg.Offset, 10),
		"x-error":              truncate(lastErr.Error(), maxErrorHeaderLength),
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deadLetterTimeout)
	defer cancel()

	if err := c.deadLetter.ProduceRaw(publishCtx, dlqTopic, msg.Key, msg.Value, headers); err != nil {
		return fmt.Errorf("kafka: dead-letter %s partition %d offset %d: %w", msg.Topic, msg.Partition, msg.Offset, err)
	}

	c.logger.Error(
		"kafka message moved to dead-letter topic",
		"topic", msg.Topic,
		"dead_letter_topic", dlqTopic,
		"partition", msg.Partition,
		"offset", msg.Offset,
		"error", lastErr,
	)
	return nil
}

func (c *Consumer) retryDelayFor(attempt int) time.Duration {
	delay := c.retryBackoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxRetryBackoff {
			return maxRetryBackoff
		}
	}
	return delay
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func (c *Consumer) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.client.Close()
	})
	return c.closeErr
}

func messageFromKafka(msg *kafka.Message) Message {
	m := Message{
		Key:       msg.Key,
		Value:     msg.Value,
		Partition: msg.TopicPartition.Partition,
		Offset:    int64(msg.TopicPartition.Offset),
		Timestamp: msg.Timestamp,
	}
	if msg.TopicPartition.Topic != nil {
		m.Topic = *msg.TopicPartition.Topic
	}
	if len(msg.Headers) > 0 {
		m.Headers = make(map[string]string, len(msg.Headers))
		for _, h := range msg.Headers {
			m.Headers[h.Key] = string(h.Value)
		}
	}
	return m
}
