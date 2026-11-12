package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/mdarkanurl/JanoLuck/internal/config"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/embeddings"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"

	_ "github.com/lib/pq"
)

func main() {
	if err := run(); err != nil {
		slog.Error("embedding backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	all := flag.Bool("all", false, "re-embed every chat, not only rows with a NULL embedding")
	batch := flag.Int("batch", 500, "maximum messages to produce before checking progress")
	flag.Parse()

	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DATABASE_URL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	producer, err := kafka.NewProducer(
		kafka.ParseBrokers(cfg.KAFKA_BROKERS),
		kafka.WithClientID(cfg.KAFKA_CLIENT_ID+"-backfill"),
	)
	if err != nil {
		return fmt.Errorf("create producer: %w", err)
	}
	defer func() {
		_ = producer.Close()
	}()

	repo := embeddings.NewRepository(database.New(db))
	ctx := context.Background()

	chats, err := repo.ListChats(ctx, *all)
	if err != nil {
		return err
	}

	produced := 0
	for _, chat := range chats {
		job := embeddings.Job{
			ChatID:  chat.ID.String(),
			Content: chat.Content,
		}
		if err := producer.Produce(ctx, embeddings.Topic, job.ChatID, job); err != nil {
			return fmt.Errorf("publish embedding job for %s: %w", job.ChatID, err)
		}
		produced++
		if *batch > 0 && produced%*batch == 0 {
			slog.Info("embedding backfill progress", "produced", produced, "total", len(chats))
		}
	}

	slog.Info("embedding backfill complete", "produced", produced, "include_all", *all)
	return nil
}
