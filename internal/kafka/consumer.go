package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	defaultPollTimeout = time.Second
	defaultConsumerID  = "janoluck-worker"
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

type Consumer struct {
	client      *kafka.Consumer
	pollTimeout time.Duration
	logger      *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

type consumerConfig struct {
	clientID    string
	pollTimeout time.Duration
	logger      *slog.Logger
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

func NewConsumer(brokers []string, groupID string, topics []string, opts ...ConsumerOption) (*Consumer, error) {
	cfg := consumerConfig{
		clientID:    defaultConsumerID,
		pollTimeout: defaultPollTimeout,
		logger:      slog.Default(),
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
		client:      client,
		pollTimeout: cfg.pollTimeout,
		logger:      cfg.logger,
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
		handlerErr := handler(ctx, m)
		if ctx.Err() != nil {
			return nil
		}
		if handlerErr != nil {
			c.logger.Error(
				"kafka message handler failed",
				"topic", m.Topic,
				"partition", m.Partition,
				"offset", m.Offset,
				"error", handlerErr,
			)
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
