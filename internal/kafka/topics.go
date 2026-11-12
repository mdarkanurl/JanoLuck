package kafka

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	defaultTopicPartitions = 3
	defaultTopicReplicas   = 1
)

type Topic struct {
	Name       string
	Partitions int
	Replicas   int
}

func (t Topic) partitions() int {
	if t.Partitions > 0 {
		return t.Partitions
	}
	return defaultTopicPartitions
}

func (t Topic) replicas() int {
	if t.Replicas > 0 {
		return t.Replicas
	}
	return defaultTopicReplicas
}

func EnsureTopics(ctx context.Context, brokers []string, topics []Topic, timeout time.Duration) error {
	brokers = normalizeBrokers(brokers)
	if len(brokers) == 0 {
		return fmt.Errorf("kafka: at least one broker is required")
	}
	if len(topics) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	admin, err := kafka.NewAdminClient(&kafka.ConfigMap{
		"bootstrap.servers": strings.Join(brokers, ","),
		"client.id":         defaultClientID + "-admin",
	})
	if err != nil {
		return fmt.Errorf("kafka: create admin client: %w", err)
	}
	defer admin.Close()

	specs := make([]kafka.TopicSpecification, 0, len(topics))
	for _, topic := range topics {
		specs = append(specs, kafka.TopicSpecification{
			Topic:             topic.Name,
			NumPartitions:     topic.partitions(),
			ReplicationFactor: topic.replicas(),
		})
	}

	results, err := admin.CreateTopics(ctx, specs, kafka.SetAdminOperationTimeout(timeout))
	if err != nil {
		return fmt.Errorf("kafka: create topics: %w", err)
	}

	for _, result := range results {
		if result.Error.Code() == kafka.ErrNoError || result.Error.Code() == kafka.ErrTopicAlreadyExists {
			continue
		}
		return fmt.Errorf("kafka: create topic %q: %w", result.Topic, result.Error)
	}

	return nil
}
