package chats

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

// fakeLanguageModel is a stand-in for the LLM service.
type fakeLanguageModel struct {
	embedding  []float64
	prediction string
	deltas     []string
	streamErr  error
	streamed   bool
	embedded   []string
}

func (f *fakeLanguageModel) GenerateEmbedding(content string) ([]float64, error) {
	f.embedded = append(f.embedded, content)

	if f.embedding != nil {
		return f.embedding, nil
	}

	return []float64{0.1, 0.2}, nil
}

func (f *fakeLanguageModel) GeneratePrediction(content string, previousChats []string, modeName, modePrompt string) (string, error) {
	return f.prediction, nil
}

func (f *fakeLanguageModel) GeneratePredictionStream(_ context.Context, _ string, _ []string, _, _ string, onChunk func(string) error) (string, error) {
	f.streamed = true

	if f.streamErr != nil {
		return "", f.streamErr
	}

	var prediction strings.Builder
	for _, delta := range f.deltas {
		prediction.WriteString(delta)

		if onChunk != nil {
			if err := onChunk(delta); err != nil {
				return prediction.String(), err
			}
		}
	}

	return prediction.String(), nil
}

// fakeStreamEmitter records the events of a streamed prediction.
type fakeStreamEmitter struct {
	started bool
	deltas  []string
	done    []Chat
	err     error
}

func (e *fakeStreamEmitter) Start() error {
	e.started = true

	return nil
}

func (e *fakeStreamEmitter) Delta(text string) error {
	e.deltas = append(e.deltas, text)

	return nil
}

func (e *fakeStreamEmitter) Done(chat Chat) error {
	e.done = append(e.done, chat)

	return nil
}

// chatFlowDB answers the queries of the chat creation flow.
func chatFlowDB() *testutil.DB {
	return &testutil.DB{
		Query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "SessionExistsByUserID"):
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			case strings.Contains(query, "GetActiveModeByUserID"):
				return testutil.QueryError(sql.ErrNoRows)
			case strings.Contains(query, "CreateChat"):
				values := chatRowValues()
				values[4] = "A bright future awaits"

				return testutil.NewRows(chatColumns, values), nil
			case strings.Contains(query, "ListSimilarChatsBySessionID"):
				return testutil.NewRows([]string{"content"}, []driver.Value{"an older message"}), nil
			case strings.Contains(query, "GetSessionSummary"):
				return testutil.QueryError(sql.ErrNoRows)
			case strings.Contains(query, "CountChatsBySessionID"):
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(2)}), nil
			default:
				return testutil.QueryError(errors.New("unexpected query"))
			}
		},
	}
}

func TestServiceCreateChatStream(t *testing.T) {
	model := &fakeLanguageModel{deltas: []string{"A bright ", "future awaits"}}
	svc := NewService(testRepository(t, chatFlowDB()), model)

	emitter := &fakeStreamEmitter{}
	reply, err := svc.CreateChatStream(context.Background(), testUserID, testSessionID, "What next?", emitter)
	if err != nil {
		t.Fatalf("CreateChatStream() error = %v", err)
	}

	if !model.streamed {
		t.Error("expected the streaming prediction to be used")
	}
	if !emitter.started {
		t.Error("expected the stream to be started")
	}
	if strings.Join(emitter.deltas, "") != "A bright future awaits" {
		t.Errorf("deltas = %v, want the full prediction", emitter.deltas)
	}
	if len(emitter.done) != 1 || emitter.done[0].ID != testChatID {
		t.Fatalf("done events = %+v, want the stored reply", emitter.done)
	}
	if reply.Content != "A bright future awaits" {
		t.Errorf("reply = %+v, want the stored prediction", reply)
	}
}

func TestServiceCreateChatStreamValidatesInput(t *testing.T) {
	svc := NewService(nil, &fakeLanguageModel{})

	cases := []struct {
		name      string
		userID    uuid.UUID
		sessionID uuid.UUID
		content   string
	}{
		{name: "bad user", userID: uuid.Nil, sessionID: testSessionID, content: "hello"},
		{name: "bad session", userID: testUserID, sessionID: uuid.Nil, content: "hello"},
		{name: "blank content", userID: testUserID, sessionID: testSessionID, content: "  "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emitter := &fakeStreamEmitter{}

			_, err := svc.CreateChatStream(context.Background(), tc.userID, tc.sessionID, tc.content, emitter)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
			if emitter.started {
				t.Error("the stream must not start for an invalid request")
			}
		})
	}
}

func TestServiceCreateChatStreamRequiresEmitter(t *testing.T) {
	svc := NewService(nil, &fakeLanguageModel{})

	_, err := svc.CreateChatStream(context.Background(), testUserID, testSessionID, "hello", nil)
	if !errors.Is(err, ErrInternal) {
		t.Errorf("error = %v, want ErrInternal", err)
	}
}

func TestServiceCreateChatStreamKeepsSessionErrorsBeforeStreaming(t *testing.T) {
	svc := NewService(testRepository(t, &testutil.DB{Query: sessionExistsHandler(false)}), &fakeLanguageModel{})

	emitter := &fakeStreamEmitter{}
	_, err := svc.CreateChatStream(context.Background(), testUserID, testSessionID, "hello", emitter)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error = %v, want ErrSessionNotFound", err)
	}
	if emitter.started {
		t.Error("the stream must not start when the session is missing")
	}
}

func TestServiceCreateChatStreamPropagatesModelErrors(t *testing.T) {
	model := &fakeLanguageModel{streamErr: errors.New("model down")}
	svc := NewService(testRepository(t, chatFlowDB()), model)

	emitter := &fakeStreamEmitter{}
	_, err := svc.CreateChatStream(context.Background(), testUserID, testSessionID, "hello", emitter)
	if !errors.Is(err, ErrInternal) {
		t.Errorf("error = %v, want ErrInternal", err)
	}
	if !emitter.started {
		t.Error("expected the stream to be started")
	}
	if len(emitter.done) != 0 {
		t.Error("expected no completion event after a failure")
	}
}

func TestCreateChatStreamHandler(t *testing.T) {
	model := &fakeLanguageModel{deltas: []string{"A bright ", "future awaits"}}
	h := NewHandler(NewService(testRepository(t, chatFlowDB()), model))

	rr := doChatRequest(t, h.CreateChatStream, http.MethodPost, `{"content":"What next?"}`, testSessionID.String(), testUserID.String())

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	events := decodeStreamEvents(t, rr.Body.String())
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %s", len(events), rr.Body.String())
	}
	if events[0].Type != StreamEventDelta || events[0].Content != "A bright " {
		t.Errorf("first event = %+v, want the first delta", events[0])
	}
	if events[1].Type != StreamEventDelta || events[1].Content != "future awaits" {
		t.Errorf("second event = %+v, want the second delta", events[1])
	}
	if events[2].Type != StreamEventDone || events[2].Chat == nil {
		t.Fatalf("last event = %+v, want the stored chat", events[2])
	}
	if events[2].Chat.ID != testChatID.String() {
		t.Errorf("chat id = %q, want %q", events[2].Chat.ID, testChatID.String())
	}
}

func TestCreateChatStreamHandlerRejectsInvalidRequests(t *testing.T) {
	h := NewHandler(NewService(testRepository(t, chatFlowDB()), &fakeLanguageModel{}))

	cases := []struct {
		name      string
		body      string
		sessionID string
		userID    string
		status    int
	}{
		{name: "malformed body", body: `{`, sessionID: testSessionID.String(), userID: testUserID.String(), status: http.StatusBadRequest},
		{name: "empty content", body: `{"content":"  "}`, sessionID: testSessionID.String(), userID: testUserID.String(), status: http.StatusBadRequest},
		{name: "missing session", body: `{"content":"hello"}`, sessionID: "", userID: testUserID.String(), status: http.StatusNotFound},
		{name: "unauthenticated", body: `{"content":"hello"}`, sessionID: testSessionID.String(), userID: "", status: http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doChatRequest(t, h.CreateChatStream, http.MethodPost, tc.body, tc.sessionID, tc.userID)
			if rr.Code != tc.status {
				t.Errorf("status = %d, want %d (body %s)", rr.Code, tc.status, rr.Body.String())
			}
		})
	}
}

func TestCreateChatStreamHandlerReportsModelFailures(t *testing.T) {
	model := &fakeLanguageModel{streamErr: errors.New("model down")}
	h := NewHandler(NewService(testRepository(t, chatFlowDB()), model))

	rr := doChatRequest(t, h.CreateChatStream, http.MethodPost, `{"content":"hello"}`, testSessionID.String(), testUserID.String())
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}

	events := decodeStreamEvents(t, rr.Body.String())
	if len(events) != 1 || events[0].Type != StreamEventError {
		t.Fatalf("events = %+v, want a single error event", events)
	}
	if events[0].Error != ErrInternal.Error() {
		t.Errorf("error = %q, want %q", events[0].Error, ErrInternal.Error())
	}
}

// decodeStreamEvents parses the server-sent events of a streamed prediction.
func decodeStreamEvents(t *testing.T, body string) []ChatStreamPayload {
	t.Helper()

	var events []ChatStreamPayload

	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		data, found := strings.CutPrefix(block, "data: ")
		if !found {
			t.Fatalf("unexpected stream block %q", block)
		}

		var event ChatStreamPayload
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatalf("decode stream event %q: %v", data, err)
		}

		events = append(events, event)
	}

	return events
}
