package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	llm "github.com/mdarkanurl/JanoLuck/internal/LLM"
	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/email"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"
	"github.com/mdarkanurl/JanoLuck/internal/sessions"

	_ "github.com/lib/pq"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DATABASE_URL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	dbReady := true
	if err := db.Ping(); err != nil {
		slog.Warn("database unavailable; session title consumer disabled", "error", err)
		dbReady = false
	}

	brokers := kafka.ParseBrokers(cfg.KAFKA_BROKERS)

	producer, err := kafka.NewProducer(
		brokers,
		kafka.WithClientID(cfg.KAFKA_CLIENT_ID+"-worker"),
	)
	if err != nil {
		slog.Warn("kafka producer unavailable; failed messages will be dropped instead of dead-lettered", "error", err)
	} else {
		defer func() {
			if closeErr := producer.Close(); closeErr != nil {
				slog.Warn("closing kafka producer", "error", closeErr)
			}
		}()
	}

	deadLetterOpts := []kafka.ConsumerOption{}
	if producer != nil {
		deadLetterOpts = append(deadLetterOpts, kafka.WithDeadLetterPublisher(producer))
	}

	emailConsumer, err := kafka.NewConsumer(
		brokers,
		cfg.KAFKA_CONSUMER_GROUP,
		[]string{email.VerificationTopic, email.PasswordResetTopic},
		append([]kafka.ConsumerOption{
			kafka.WithConsumerClientID(cfg.KAFKA_CLIENT_ID + "-email-worker"),
			kafka.WithMaxAttempts(1),
		}, deadLetterOpts...)...,
	)
	if err != nil {
		return fmt.Errorf("create email consumer: %w", err)
	}
	defer func() {
		_ = emailConsumer.Close()
	}()

	emailWorker := email.NewWorker(email.NewResendService(cfg.RESEND_API_KEY))

	consumers := []consumer{
		{
			name: "email",
			run: func(ctx context.Context) error {
				return emailConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return emailWorker.Handle(ctx, msg.Topic, msg.Value, msg.Timestamp)
				})
			},
		},
	}

	if dbReady {
		titleConsumer, err := kafka.NewConsumer(
			brokers,
			cfg.KAFKA_SESSION_CONSUMER_GROUP,
			[]string{sessions.TitleTopic},
			append([]kafka.ConsumerOption{
				kafka.WithConsumerClientID(cfg.KAFKA_CLIENT_ID + "-session-worker"),
				kafka.WithMaxAttempts(3),
			}, deadLetterOpts...)...,
		)
		if err != nil {
			return fmt.Errorf("create session title consumer: %w", err)
		}
		defer func() {
			_ = titleConsumer.Close()
		}()

		titleWorker := sessions.NewTitleWorker(
			sessions.NewRepository(database.New(db)),
			llm.NewOpenRouterService(cfg.LLM_API_KEY, cfg.LLM_MODEL_NAME, cfg.LLM_EMBED_MODEL_NAME),
		)

		consumers = append(consumers, consumer{
			name: "session-title",
			run: func(ctx context.Context) error {
				return titleConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return titleWorker.Handle(ctx, msg.Value, msg.Timestamp)
				})
			},
		})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("worker started",
		"email_topics", []string{email.VerificationTopic, email.PasswordResetTopic},
		"email_group", cfg.KAFKA_CONSUMER_GROUP,
		"title_topic", sessions.TitleTopic,
		"title_group", cfg.KAFKA_SESSION_CONSUMER_GROUP,
		"title_enabled", dbReady,
		"brokers", cfg.KAFKA_BROKERS,
	)

	return runConsumers(ctx, stop, consumers)
}

type consumer struct {
	name string
	run  func(ctx context.Context) error
}

func runConsumers(ctx context.Context, cancel context.CancelFunc, consumers []consumer) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(consumers))

	for _, c := range consumers {
		wg.Add(1)
		go func(c consumer) {
			defer wg.Done()
			if err := c.run(ctx); err != nil {
				errCh <- fmt.Errorf("%s consumer: %w", c.name, err)
			}
		}(c)
	}

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errCh:
		cancel()
	}

	wg.Wait()

	if runErr == nil {
		select {
		case runErr = <-errCh:
		default:
		}
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	slog.Info("worker stopped")
	return nil
}
