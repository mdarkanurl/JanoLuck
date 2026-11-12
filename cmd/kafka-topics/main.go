package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"
	"github.com/mdarkanurl/JanoLuck/internal/topics"
)

func main() {
	if err := run(); err != nil {
		slog.Error("topic provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	partitions := flag.Int("partitions", 3, "number of partitions per topic")
	replicas := flag.Int("replicas", 1, "number of replicas per topic")
	timeout := flag.Duration("timeout", 30*time.Second, "how long to wait for the broker")
	flag.Parse()

	cfg := config.Load()

	all := topics.All()
	specs := make([]kafka.Topic, 0, len(all))
	for _, topic := range all {
		specs = append(specs, kafka.Topic{
			Name:       topic.Name,
			Partitions: *partitions,
			Replicas:   *replicas,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := kafka.EnsureTopics(ctx, kafka.ParseBrokers(cfg.KAFKA_BROKERS), specs, *timeout); err != nil {
		return fmt.Errorf("ensure topics: %w", err)
	}

	slog.Info("topics provisioned", "count", len(specs), "partitions", *partitions)
	return nil
}
