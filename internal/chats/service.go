package chats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/analytics"
	"github.com/mdarkanurl/JanoLuck/internal/embeddings"
	"github.com/mdarkanurl/JanoLuck/internal/httpx"
	"github.com/mdarkanurl/JanoLuck/internal/moderation"
	"github.com/mdarkanurl/JanoLuck/internal/outbox"
	"github.com/mdarkanurl/JanoLuck/internal/sessions"
)

var (
	ErrInvalidInput    = errors.New("invalid input")
	ErrInternal        = errors.New("something went wrong")
	ErrNotFound        = errors.New("chat not found")
	ErrSessionNotFound = errors.New("session not found")
	ErrNotFirstChat    = errors.New("It is not first chat")
	ErrNoActiveMode    = errors.New("no active mode")
)

type Publisher interface {
	Produce(ctx context.Context, topic, key string, value any) error
	ProduceAsync(ctx context.Context, topic, key string, value any) error
	ProduceRawAsync(ctx context.Context, topic string, key, value []byte, headers map[string]string) error
}

// LanguageModel is the part of the LLM service the chat flow depends on.
type LanguageModel interface {
	GenerateEmbedding(ctx context.Context, content string) ([]float64, error)
	GeneratePrediction(ctx context.Context, content string, previousChats []string, modeName, modePrompt string) (string, error)
	GeneratePredictionStream(ctx context.Context, content string, previousChats []string, modeName, modePrompt string, onChunk func(string) error) (string, error)
}

// StreamEmitter receives the events of a streamed prediction. Start is called
// once the request is accepted, Delta for every fragment produced by the
// model and Done with the stored reply once the prediction is complete.
type StreamEmitter interface {
	Start() error
	Delta(text string) error
	Done(chat Chat) error
}

type Service struct {
	repo     *Repository
	llm      LanguageModel
	producer Publisher
	store    *outbox.Repository
	logger   *slog.Logger
}

type ServiceOption func(*Service)

func WithProducer(producer Publisher) ServiceOption {
	return func(s *Service) {
		if producer != nil {
			s.producer = producer
		}
	}
}

func WithEventStore(store *outbox.Repository) ServiceOption {
	return func(s *Service) {
		if store != nil {
			s.store = store
		}
	}
}

func WithLogger(logger *slog.Logger) ServiceOption {
	return func(s *Service) {
		if logger != nil {
			s.logger = logger
		}
	}
}

const (
	DefaultPage  = httpx.DefaultPage
	DefaultLimit = httpx.DefaultLimit
	MaxPage      = 1_000_000
	MaxLimit     = 100

	similarChatsLimit = 10
	publishTimeout    = 5 * time.Second
)

func NewService(repo *Repository, model LanguageModel, opts ...ServiceOption) *Service {
	s := &Service{
		repo:   repo,
		llm:    model,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// chatPrep holds the validated request and the context needed to answer it.
// Nothing is written to the database until the prediction has been generated.
type chatPrep struct {
	userID        uuid.UUID
	sessionID     uuid.UUID
	content       string
	embedding     string
	modeName      string
	modePrompt    string
	previousChats []string
}

func (s *Service) CreateChat(ctx context.Context, userIDStr, sessionIDStr uuid.UUID, content string) (Chat, error) {
	prep, err := s.prepareChat(ctx, userIDStr, sessionIDStr, content)
	if err != nil {
		return Chat{}, err
	}

	prediction, err := s.llm.GeneratePrediction(ctx, prep.content, prep.previousChats, prep.modeName, prep.modePrompt)
	if err != nil {
		return Chat{}, fmt.Errorf("%w: llm GeneratePrediction: %v", ErrInternal, err)
	}

	reply, err := s.storeExchange(ctx, prep, prediction)
	if err != nil {
		return Chat{}, err
	}

	s.enqueueSummary(ctx, prep.sessionID, prep.userID)

	return reply, nil
}

func (s *Service) CreateChatStream(ctx context.Context, userID, sessionID uuid.UUID, content string, emitter StreamEmitter) (Chat, error) {
	if emitter == nil {
		return Chat{}, fmt.Errorf("%w: stream emitter is required", ErrInternal)
	}

	prep, err := s.prepareChat(ctx, userID, sessionID, content)
	if err != nil {
		return Chat{}, err
	}

	if err := emitter.Start(); err != nil {
		return Chat{}, fmt.Errorf("%w: start stream: %v", ErrInternal, err)
	}

	prediction, err := s.llm.GeneratePredictionStream(ctx, prep.content, prep.previousChats, prep.modeName, prep.modePrompt, func(chunk string) error {
		return emitter.Delta(chunk)
	})
	if err != nil {
		return Chat{}, fmt.Errorf("%w: llm GeneratePrediction: %v", ErrInternal, err)
	}

	reply, err := s.storeExchange(ctx, prep, prediction)
	if err != nil {
		return Chat{}, err
	}

	s.enqueueSummary(ctx, prep.sessionID, prep.userID)

	if err := emitter.Done(reply); err != nil {
		return Chat{}, fmt.Errorf("%w: finish stream: %v", ErrInternal, err)
	}

	return reply, nil
}

func (s *Service) prepareChat(ctx context.Context, userID, sessionID uuid.UUID, content string) (chatPrep, error) {
	if userID == uuid.Nil {
		return chatPrep{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	if sessionID == uuid.Nil {
		return chatPrep{}, fmt.Errorf("%w: invalid session id", ErrInvalidInput)
	}

	normalizedContent, err := normalizeMessage(content)
	if err != nil {
		return chatPrep{}, err
	}
	content = normalizedContent

	if err := s.ensureSessionExists(ctx, sessionID, userID); err != nil {
		return chatPrep{}, err
	}

	var modeName, modePrompt string
	activeMode, err := s.repo.GetActiveModeByUserID(ctx, userID)
	if err != nil && !errors.Is(err, ErrNoActiveMode) {
		return chatPrep{}, err
	}
	if err == nil {
		modeName = activeMode.Name
		modePrompt = activeMode.Prompt
	}

	embedding, err := s.llm.GenerateEmbedding(ctx, content)
	if err != nil {
		return chatPrep{}, fmt.Errorf("%w: llm GenerateEmbedding: %v", ErrInternal, err)
	}
	embeddingString := vectorString(embedding)

	previousChats, err := s.repo.ListSimilarChatsBySessionID(ctx, sessionID, uuid.Nil, embeddingString, similarChatsLimit)
	if err != nil {
		return chatPrep{}, err
	}

	summary, err := s.repo.GetSessionSummary(ctx, sessionID)
	if err != nil {
		return chatPrep{}, err
	}
	if summary = strings.TrimSpace(summary); summary != "" {
		previousChats = append([]string{"Summary of the earlier conversation: " + summary}, previousChats...)
	}

	return chatPrep{
		userID:        userID,
		sessionID:     sessionID,
		content:       content,
		embedding:     embeddingString,
		modeName:      modeName,
		modePrompt:    modePrompt,
		previousChats: previousChats,
	}, nil
}

func (s *Service) storeExchange(ctx context.Context, prep chatPrep, prediction string) (Chat, error) {
	predictionEmbedding, err := s.llm.GenerateEmbedding(ctx, prediction)
	if err != nil {
		return Chat{}, fmt.Errorf("%w: llm GenerateEmbedding: %v", ErrInternal, err)
	}

	messages := []NewChat{
		{Role: RoleUser, Content: prep.content, Embedding: prep.embedding},
		{Role: RoleSystem, Content: prediction, Embedding: vectorString(predictionEmbedding)},
	}

	chats, err := s.createChats(ctx, prep, messages)
	if err != nil {
		return Chat{}, err
	}

	return chats[1], nil
}

func (s *Service) createChats(ctx context.Context, prep chatPrep, messages []NewChat) ([]Chat, error) {
	build := func(index int, chat Chat) []outbox.Event {
		if index == 0 {
			return []outbox.Event{
				{
					Topic: moderation.Topic,
					Key:   chat.ID.String(),
					Payload: moderation.Job{
						Kind:    moderation.KindChat,
						ID:      chat.ID.String(),
						UserID:  prep.userID.String(),
						Content: prep.content,
					},
				},
				{
					Topic: analytics.Topic,
					Key:   prep.userID.String(),
					Payload: analytics.Event{
						Type:      "chat.user_created",
						UserID:    prep.userID.String(),
						SessionID: prep.sessionID.String(),
						ChatID:    chat.ID.String(),
					},
				},
			}
		}

		return []outbox.Event{
			{
				Topic: analytics.Topic,
				Key:   prep.userID.String(),
				Payload: analytics.Event{
					Type:      "chat.system_created",
					UserID:    prep.userID.String(),
					SessionID: prep.sessionID.String(),
					ChatID:    chat.ID.String(),
				},
			},
		}
	}

	if s.store == nil {
		chats, err := s.repo.CreateChats(ctx, prep.sessionID, prep.userID, messages)
		if err != nil {
			return nil, err
		}

		for i, chat := range chats {
			for _, event := range build(i, chat) {
				s.publishAsync(ctx, event.Topic, event.Key, event.Payload, event.Headers)
			}
		}

		return chats, nil
	}

	return s.repo.CreateChatsWithEvents(ctx, prep.sessionID, prep.userID, messages, build)
}

func (s *Service) enqueueSummary(ctx context.Context, sessionID, userID uuid.UUID) {
	if s.producer == nil && s.store == nil {
		return
	}

	total, err := s.repo.CountChatsBySessionID(ctx, sessionID, userID)
	if err != nil {
		s.logger.Warn("could not count session chats for summary", "session_id", sessionID, "error", err)
		return
	}
	if total == 0 || total%sessions.SummaryBatchSize != 0 {
		return
	}

	s.emit(ctx, outbox.Event{
		Topic: sessions.SummaryTopic,
		Key:   sessionID.String(),
		Payload: sessions.SummaryJob{
			SessionID: sessionID.String(),
			UserID:    userID.String(),
		},
	})
}

func (s *Service) emit(ctx context.Context, event outbox.Event) {
	if s.store != nil {
		if err := s.store.Enqueue(ctx, event.Topic, event.Key, event.Payload, event.Headers); err != nil {
			s.logger.Warn("could not enqueue event", "topic", event.Topic, "key", event.Key, "error", err)
		}
		return
	}

	s.publishAsync(ctx, event.Topic, event.Key, event.Payload, event.Headers)
}

func (s *Service) publishAsync(ctx context.Context, topic, key string, value any, headers map[string]string) {
	if s.producer == nil {
		return
	}

	payload, err := json.Marshal(value)
	if err != nil {
		s.logger.Warn("could not encode chat event", "topic", topic, "key", key, "error", err)
		return
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()

	if err := s.producer.ProduceRawAsync(publishCtx, topic, []byte(key), payload, headers); err != nil {
		s.logger.Warn("could not publish chat event", "topic", topic, "key", key, "error", err)
	}
}

func vectorString(v []float64) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, f := range v {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%g", f))
	}
	sb.WriteString("]")
	return sb.String()
}

func (s *Service) ListChats(ctx context.Context, userIDStr, sessionIDStr string, page, limit int) (ListResult, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return ListResult{}, err
	}

	sessionID, err := parseUUID(sessionIDStr, "session id")
	if err != nil {
		return ListResult{}, err
	}

	if err := s.ensureSessionExists(ctx, sessionID, userID); err != nil {
		return ListResult{}, err
	}

	if page < 1 || page > MaxPage {
		return ListResult{}, fmt.Errorf("%w: page must be between 1 and %d", ErrInvalidInput, MaxPage)
	}
	if limit < 1 || limit > MaxLimit {
		return ListResult{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidInput, MaxLimit)
	}

	total, err := s.repo.CountChatsBySessionID(ctx, sessionID, userID)
	if err != nil {
		return ListResult{}, err
	}

	chats, err := s.repo.ListChatsBySessionID(ctx, sessionID, userID, limit, (page-1)*limit)
	if err != nil {
		return ListResult{}, err
	}

	return ListResult{Chats: chats, Total: total}, nil
}

func (s *Service) GetChat(ctx context.Context, userIDStr, chatIDStr string) (Chat, error) {
	userID, chatID, err := parseIDs(userIDStr, chatIDStr, "chat id")
	if err != nil {
		return Chat{}, err
	}

	return s.repo.GetChatByID(ctx, chatID, userID)
}

func (s *Service) UpdateChat(ctx context.Context, userIDStr, chatIDStr, content string) (Chat, error) {
	userID, chatID, err := parseIDs(userIDStr, chatIDStr, "chat id")
	if err != nil {
		return Chat{}, err
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return Chat{}, fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	if len(content) > 8000 {
		return Chat{}, fmt.Errorf("%w: content must be at most 8000 characters", ErrInvalidInput)
	}

	build := func(chat Chat) []outbox.Event {
		return []outbox.Event{
			{
				Topic: embeddings.Topic,
				Key:   chatID.String(),
				Payload: embeddings.Job{
					ChatID:  chatID.String(),
					Content: content,
				},
			},
			{
				Topic: moderation.Topic,
				Key:   chatID.String(),
				Payload: moderation.Job{
					Kind:    moderation.KindChat,
					ID:      chatID.String(),
					UserID:  userID.String(),
					Content: content,
				},
			},
			{
				Topic: analytics.Topic,
				Key:   userID.String(),
				Payload: analytics.Event{
					Type:      "chat.updated",
					UserID:    userID.String(),
					SessionID: chat.SessionID.String(),
					ChatID:    chatID.String(),
				},
			},
		}
	}

	if s.store != nil {
		return s.repo.UpdateChatContentWithEvents(ctx, chatID, userID, content, build)
	}

	updated, err := s.repo.UpdateChatContent(ctx, chatID, userID, content)
	if err != nil {
		return Chat{}, err
	}

	for _, event := range build(updated) {
		s.publishAsync(ctx, event.Topic, event.Key, event.Payload, event.Headers)
	}

	return updated, nil
}

func (s *Service) DeleteChat(ctx context.Context, userIDStr, chatIDStr string) error {
	userID, chatID, err := parseIDs(userIDStr, chatIDStr, "chat id")
	if err != nil {
		return err
	}

	return s.repo.DeleteChat(ctx, chatID, userID)
}

func (s *Service) ensureSessionExists(ctx context.Context, sessionID, userID uuid.UUID) error {
	exists, err := s.repo.SessionExistsByUserID(ctx, sessionID, userID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrSessionNotFound
	}

	return nil
}

func normalizeMessage(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("%w: content is required", ErrInvalidInput)
	}
	if len(content) > 8000 {
		return "", fmt.Errorf("%w: content must be at most 8000 characters", ErrInvalidInput)
	}

	return content, nil
}

func parseIDs(userIDStr, chatIDStr, field string) (uuid.UUID, uuid.UUID, error) {
	userID, err := parseUUID(userIDStr, "user id")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	chatID, err := parseUUID(chatIDStr, field)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return userID, chatID, nil
}

func parseUUID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: invalid %s", ErrInvalidInput, field)
	}

	return id, nil
}
