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
	"time"

	llm "github.com/mdarkanurl/JanoLuck/internal/LLM"
	"github.com/mdarkanurl/JanoLuck/internal/analytics"
	"github.com/mdarkanurl/JanoLuck/internal/audit"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/email"
	"github.com/mdarkanurl/JanoLuck/internal/embeddings"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"
	"github.com/mdarkanurl/JanoLuck/internal/moderation"
	"github.com/mdarkanurl/JanoLuck/internal/notifications"
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/sessions"
	"github.com/mdarkanurl/JanoLuck/internal/topics"

	_ "github.com/lib/pq"
)

const topicProvisionTimeout = 15 * time.Second

var authTopics = []string{
	auth.UserRegisteredTopic,
	auth.LoginSucceededTopic,
	auth.UserPasswordResetTopic,
	auth.UserPasswordChangedTopic,
	auth.LogoutTopic,
}

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
		slog.Warn("database unavailable; database-backed consumers disabled", "error", err)
		dbReady = false
	}

	brokers := kafka.ParseBrokers(cfg.KAFKA_BROKERS)

	provisionCtx, cancelProvision := context.WithTimeout(context.Background(), topicProvisionTimeout)
	if err := kafka.EnsureTopics(provisionCtx, brokers, topics.All(), topicProvisionTimeout); err != nil {
		slog.Warn("could not provision kafka topics; relying on broker auto-creation", "error", err)
	}
	cancelProvision()

	producer, err := kafka.NewProducer(
		brokers,
		kafka.WithClientID(cfg.KAFKA_CLIENT_ID+"-worker"),
	)
	if err != nil {
		slog.Warn("kafka producer unavailable; failed messages will be dropped instead of dead-lettered", "error", err)
		producer = nil
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

	dbQueries := database.New(db)
	llmService := llm.NewOpenRouterService(cfg.LLM_API_KEY, cfg.LLM_MODEL_NAME, cfg.LLM_EMBED_MODEL_NAME)

	consumers := []consumer{}

	emailConsumer, err := kafka.NewConsumer(
		brokers,
		cfg.KAFKA_CONSUMER_GROUP,
		[]string{email.VerificationTopic, email.PasswordResetTopic, email.NotificationTopic},
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
	consumers = append(consumers, consumer{
		name: "email",
		run: func(ctx context.Context) error {
			return emailConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
				return emailWorker.Handle(ctx, msg.Topic, msg.Value, msg.Timestamp)
			})
		},
	})

	analyticsConsumer, err := newConsumer(brokers, cfg.KAFKA_CLIENT_ID+"-analytics",
		[]string{analytics.Topic}, cfg.KAFKA_CLIENT_ID+"-analytics-worker", 1, deadLetterOpts)
	if err != nil {
		return err
	}
	defer func() {
		_ = analyticsConsumer.Close()
	}()

	analyticsWorker := analytics.NewWorker(nil)
	consumers = append(consumers, consumer{
		name: "analytics",
		run: func(ctx context.Context) error {
			return analyticsConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
				return analyticsWorker.Handle(ctx, msg.Value)
			})
		},
	})

	if producer != nil {
		notificationConsumer, err := newConsumer(brokers, cfg.KAFKA_CLIENT_ID+"-notifications",
			authTopics, cfg.KAFKA_CLIENT_ID+"-notifications-worker", 3, deadLetterOpts)
		if err != nil {
			return err
		}
		defer func() {
			_ = notificationConsumer.Close()
		}()

		notificationWorker := notifications.NewWorker(producer, nil)
		consumers = append(consumers, consumer{
			name: "notifications",
			run: func(ctx context.Context) error {
				return notificationConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return notificationWorker.Handle(ctx, msg.Topic, msg.Value)
				})
			},
		})
	}

	if dbReady {
		titleConsumer, err := newConsumer(brokers, cfg.KAFKA_SESSION_CONSUMER_GROUP,
			[]string{sessions.TitleTopic}, cfg.KAFKA_CLIENT_ID+"-session-worker", 3, deadLetterOpts)
		if err != nil {
			return err
		}
		defer func() {
			_ = titleConsumer.Close()
		}()

		titleWorker := sessions.NewTitleWorker(sessions.NewRepository(dbQueries), llmService)
		consumers = append(consumers, consumer{
			name: "session-title",
			run: func(ctx context.Context) error {
				return titleConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return titleWorker.Handle(ctx, msg.Value, msg.Timestamp)
				})
			},
		})

		summaryConsumer, err := newConsumer(brokers, cfg.KAFKA_SESSION_CONSUMER_GROUP,
			[]string{sessions.SummaryTopic}, cfg.KAFKA_CLIENT_ID+"-summary-worker", 3, deadLetterOpts)
		if err != nil {
			return err
		}
		defer func() {
			_ = summaryConsumer.Close()
		}()

		summaryWorker := sessions.NewSummaryWorker(sessions.NewRepository(dbQueries), llmService)
		consumers = append(consumers, consumer{
			name: "session-summary",
			run: func(ctx context.Context) error {
				return summaryConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return summaryWorker.Handle(ctx, msg.Value)
				})
			},
		})

		auditConsumer, err := newConsumer(brokers, cfg.KAFKA_CLIENT_ID+"-audit",
			authTopics, cfg.KAFKA_CLIENT_ID+"-audit-worker", 3, deadLetterOpts)
		if err != nil {
			return err
		}
		defer func() {
			_ = auditConsumer.Close()
		}()

		auditWorker := audit.NewWorker(audit.NewRepository(dbQueries), nil)
		consumers = append(consumers, consumer{
			name: "audit",
			run: func(ctx context.Context) error {
				return auditConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return auditWorker.Handle(ctx, msg.Topic, msg.Value)
				})
			},
		})

		embeddingConsumer, err := newConsumer(brokers, cfg.KAFKA_CLIENT_ID+"-embedding",
			[]string{embeddings.Topic}, cfg.KAFKA_CLIENT_ID+"-embedding-worker", 3, deadLetterOpts)
		if err != nil {
			return err
		}
		defer func() {
			_ = embeddingConsumer.Close()
		}()

		embeddingWorker := embeddings.NewWorker(embeddings.NewRepository(dbQueries), llmService, nil)
		consumers = append(consumers, consumer{
			name: "embeddings",
			run: func(ctx context.Context) error {
				return embeddingConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return embeddingWorker.Handle(ctx, msg.Value)
				})
			},
		})

		moderationConsumer, err := newConsumer(brokers, cfg.KAFKA_CLIENT_ID+"-moderation",
			[]string{moderation.Topic}, cfg.KAFKA_CLIENT_ID+"-moderation-worker", 3, deadLetterOpts)
		if err != nil {
			return err
		}
		defer func() {
			_ = moderationConsumer.Close()
		}()

		moderationWorker := moderation.NewWorker(moderation.NewRepository(dbQueries), llmService, nil)
		consumers = append(consumers, consumer{
			name: "moderation",
			run: func(ctx context.Context) error {
				return moderationConsumer.Run(ctx, func(ctx context.Context, msg kafka.Message) error {
					return moderationWorker.Handle(ctx, msg.Value)
				})
			},
		})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var relay *outbox.Relay
	if dbReady && producer != nil {
		relay = outbox.NewRelay(outbox.NewRepository(dbQueries), producer)
	}

	slog.Info("worker started",
		"brokers", cfg.KAFKA_BROKERS,
		"database_ready", dbReady,
		"consumers", len(consumers),
		"outbox_relay", relay != nil,
	)

	return runConsumers(ctx, stop, consumers, relay)
}

func newConsumer(brokers []string, groupID string, topics []string, clientID string, maxAttempts int, deadLetterOpts []kafka.ConsumerOption) (*kafka.Consumer, error) {
	opts := []kafka.ConsumerOption{
		kafka.WithConsumerClientID(clientID),
		kafka.WithMaxAttempts(maxAttempts),
	}
	opts = append(opts, deadLetterOpts...)

	c, err := kafka.NewConsumer(brokers, groupID, topics, opts...)
	if err != nil {
		return nil, fmt.Errorf("create consumer %q: %w", clientID, err)
	}

	return c, nil
}

type consumer struct {
	name string
	run  func(ctx context.Context) error
}

func runConsumers(ctx context.Context, cancel context.CancelFunc, consumers []consumer, relay *outbox.Relay) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(consumers)+1)

	for _, c := range consumers {
		wg.Add(1)
		go func(c consumer) {
			defer wg.Done()
			if err := c.run(ctx); err != nil {
				errCh <- fmt.Errorf("%s consumer: %w", c.name, err)
			}
		}(c)
	}

	if relay != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := relay.Run(ctx); err != nil {
				errCh <- fmt.Errorf("outbox relay: %w", err)
			}
		}()
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
