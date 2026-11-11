package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/email"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"
)

func main() {
	if err := run(); err != nil {
		slog.Error("email worker failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	consumer, err := kafka.NewConsumer(
		kafka.ParseBrokers(cfg.KAFKA_BROKERS),
		cfg.KAFKA_CONSUMER_GROUP,
		[]string{email.VerificationTopic, email.PasswordResetTopic},
		kafka.WithConsumerClientID(cfg.KAFKA_CLIENT_ID+"-worker"),
	)
	if err != nil {
		return err
	}
	defer func() {
		_ = consumer.Close()
	}()

	worker := email.NewWorker(email.NewResendService(cfg.RESEND_API_KEY))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("email worker started",
		"topics", []string{email.VerificationTopic, email.PasswordResetTopic},
		"group", cfg.KAFKA_CONSUMER_GROUP,
		"brokers", cfg.KAFKA_BROKERS,
	)

	if err := consumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
		return worker.Handle(ctx, msg.Topic, msg.Value, msg.Timestamp)
	}); err != nil {
		return err
	}

	slog.Info("email worker stopped")
	return nil
}
