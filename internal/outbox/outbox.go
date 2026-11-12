package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/database"
)

const (
	defaultBatchSize = 100
	defaultInterval  = 2 * time.Second
	schemaVersion    = "1"
)

func defaultHeaders() map[string]string {
	return map[string]string{
		"content_type":   "application/json",
		"schema_version": schemaVersion,
	}
}

type Message struct {
	ID      int64
	Topic   string
	Key     string
	Payload []byte
	Headers map[string]string
}

type Event struct {
	Topic   string
	Key     string
	Payload any
	Headers map[string]string
}

type Repository struct {
	db *database.Queries
}

func NewRepository(dbQueries *database.Queries) *Repository {
	return &Repository{db: dbQueries}
}

func (r *Repository) Enqueue(ctx context.Context, topic, key string, payload any, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: marshal payload for %q: %w", topic, err)
	}

	merged := defaultHeaders()
	for name, value := range headers {
		merged[name] = value
	}

	headerJSON, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("outbox: marshal headers for %q: %w", topic, err)
	}

	if _, err := r.db.EnqueueOutbox(ctx, database.EnqueueOutboxParams{
		Topic:   topic,
		Key:     key,
		Payload: body,
		Headers: headerJSON,
	}); err != nil {
		return fmt.Errorf("outbox: db EnqueueOutbox: %w", err)
	}

	return nil
}

func (r *Repository) ListPending(ctx context.Context, limit int) ([]Message, error) {
	rows, err := r.db.ListPendingOutbox(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("outbox: db ListPendingOutbox: %w", err)
	}

	messages := make([]Message, 0, len(rows))
	for _, row := range rows {
		headers := map[string]string{}
		if len(row.Headers) > 0 {
			if err := json.Unmarshal(row.Headers, &headers); err != nil {
				return nil, fmt.Errorf("outbox: decode headers for message %d: %w", row.ID, err)
			}
		}

		messages = append(messages, Message{
			ID:      row.ID,
			Topic:   row.Topic,
			Key:     row.Key,
			Payload: row.Payload,
			Headers: headers,
		})
	}

	return messages, nil
}

func (r *Repository) MarkPublished(ctx context.Context, id int64) error {
	if _, err := r.db.MarkOutboxPublished(ctx, id); err != nil {
		return fmt.Errorf("outbox: db MarkOutboxPublished: %w", err)
	}

	return nil
}

type Store interface {
	ListPending(ctx context.Context, limit int) ([]Message, error)
	MarkPublished(ctx context.Context, id int64) error
}

type Publisher interface {
	ProduceRaw(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
}

type Relay struct {
	store     Store
	publisher Publisher
	batch     int
	interval  time.Duration
	logger    *slog.Logger
}

type RelayOption func(*Relay)

func WithBatchSize(size int) RelayOption {
	return func(r *Relay) {
		if size > 0 {
			r.batch = size
		}
	}
}

func WithInterval(interval time.Duration) RelayOption {
	return func(r *Relay) {
		if interval > 0 {
			r.interval = interval
		}
	}
}

func WithLogger(logger *slog.Logger) RelayOption {
	return func(r *Relay) {
		if logger != nil {
			r.logger = logger
		}
	}
}

func NewRelay(store Store, publisher Publisher, opts ...RelayOption) *Relay {
	r := &Relay{
		store:     store,
		publisher: publisher,
		batch:     defaultBatchSize,
		interval:  defaultInterval,
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.RunOnce(ctx); err != nil {
				r.logger.Warn("outbox relay failed", "error", err)
			}
		}
	}
}

func (r *Relay) RunOnce(ctx context.Context) error {
	if r.store == nil || r.publisher == nil {
		return fmt.Errorf("outbox: relay is not configured")
	}

	messages, err := r.store.ListPending(ctx, r.batch)
	if err != nil {
		return err
	}

	for _, message := range messages {
		if err := r.publisher.ProduceRaw(ctx, message.Topic, []byte(message.Key), message.Payload, message.Headers); err != nil {
			return fmt.Errorf("outbox: publish message %d to %q: %w", message.ID, message.Topic, err)
		}
		if err := r.store.MarkPublished(ctx, message.ID); err != nil {
			return err
		}
	}

	if len(messages) > 0 {
		r.logger.Info("outbox relay published messages", "count", len(messages))
	}

	return nil
}
