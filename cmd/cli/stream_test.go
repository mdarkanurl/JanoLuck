package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func writeStreamEvent(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode stream event: %v", err)
	}

	if _, err := w.Write([]byte("data: " + string(raw) + "\n\n")); err != nil {
		t.Fatalf("write stream event: %v", err)
	}

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func TestClientCreateChatStream(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("accept = %q, want text/event-stream", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-access" {
			t.Errorf("authorization = %q, want Bearer test-access", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		writeStreamEvent(t, w, chatStreamEvent{Type: "delta", Content: "A bright "})
		writeStreamEvent(t, w, chatStreamEvent{Type: "delta", Content: "future awaits"})
		writeStreamEvent(t, w, chatStreamEvent{
			Type: "done",
			Chat: chatData{ID: "c1", Role: "assistant", Content: "A bright future awaits"},
		})
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds.AccessToken = "test-access"

	var deltas []string
	chat, err := c.createChatStream(t.Context(), "s1", "What next?", func(delta string) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("createChatStream: %v", err)
	}

	if strings.Join(deltas, "") != "A bright future awaits" {
		t.Errorf("deltas = %v, want the full prediction", deltas)
	}
	if chat.ID != "c1" || chat.Content != "A bright future awaits" {
		t.Errorf("chat = %+v, want the stored prediction", chat)
	}

	want := []string{"POST /api/v1/sessions/s1/chats/stream"}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestClientCreateChatStreamReportsServerErrors(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		writeStreamEvent(t, w, chatStreamEvent{Type: "error", Error: "something went wrong"})
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds.AccessToken = "test-access"

	if _, err := c.createChatStream(t.Context(), "s1", "hello", nil); err == nil || !strings.Contains(err.Error(), "something went wrong") {
		t.Fatalf("error = %v, want the server error", err)
	}
}

func TestClientCreateChatStreamFallsBackToTheRegularEndpoint(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)

		if r.URL.Path == "/api/v1/sessions/s1/chats/stream" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("404 page not found\n"))
			return
		}

		writeTestJSON(w, http.StatusCreated, chatResponse{
			Message: "message sent",
			Data:    chatData{ID: "c1", Role: "assistant", Content: "A bright future awaits"},
		})
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds.AccessToken = "test-access"

	chat, err := c.createChatStream(t.Context(), "s1", "hello", nil)
	if err != nil {
		t.Fatalf("createChatStream: %v", err)
	}
	if chat.ID != "c1" {
		t.Errorf("chat = %+v, want the fallback answer", chat)
	}

	want := []string{
		"POST /api/v1/sessions/s1/chats/stream",
		"POST /api/v1/sessions/s1/chats",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestClientCreateChatStreamKeepsNotFoundErrors(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds.AccessToken = "test-access"

	_, err = c.createChatStream(t.Context(), "s1", "hello", nil)
	if err == nil || !isNotFound(err) {
		t.Fatalf("error = %v, want a not found error", err)
	}
}

func TestChatStreamShowsThePredictionWhileItArrives(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.current = &sessionData{ID: "s1", Name: "Moving to a new city"}
	m.loading = true
	m.loadingText = "Jano is thinking..."

	updated, cmd := m.Update(chatStreamMsg{delta: "A bright "})
	if cmd == nil {
		t.Fatal("expected the model to wait for the next fragment")
	}
	if updated.streamText != "A bright " {
		t.Errorf("stream text = %q, want the received fragment", updated.streamText)
	}

	updated, _ = updated.Update(chatStreamMsg{delta: "future awaits"})
	if updated.streamText != "A bright future awaits" {
		t.Errorf("stream text = %q, want the joined fragments", updated.streamText)
	}

	updated.renderMessages()
	if got := updated.viewport.View(); !strings.Contains(got, "A bright future awaits") {
		t.Fatalf("the streamed prediction is missing from the view:\n%s", got)
	}

	updated, _ = updated.Update(chatStreamMsg{
		done:       true,
		prediction: chatData{Role: "assistant", Content: "A bright future awaits"},
	})
	if updated.loading {
		t.Error("expected the loading state to end with the prediction")
	}
	if updated.streamText != "" {
		t.Errorf("stream text = %q, want it cleared", updated.streamText)
	}
	if len(updated.chats) != 1 || updated.chats[0].Content != "A bright future awaits" {
		t.Errorf("chats = %+v, want the stored prediction", updated.chats)
	}
}

func TestChatStreamFailureShowsAnError(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	c, err := newClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.current = &sessionData{ID: "s1", Name: "Moving to a new city"}
	m.loading = true

	updated, _ := m.Update(chatStreamMsg{done: true, err: errors.New("the prediction failed")})
	if updated.loading {
		t.Error("expected the loading state to end with the failure")
	}
	if len(updated.chats) != 1 || updated.chats[0].Role != "error" {
		t.Errorf("chats = %+v, want an error message", updated.chats)
	}
	if updated.chats[0].CreatedAt == "" {
		t.Error("expected the error message to be timestamped")
	}
}

func TestChatStreamCommandsDeliverThePrediction(t *testing.T) {
	t.Setenv("JANOLUCK_CONFIG", filepath.Join(t.TempDir(), "auth.json"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/s1/chats/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			writeStreamEvent(t, w, chatStreamEvent{Type: "delta", Content: "A bright "})
			writeStreamEvent(t, w, chatStreamEvent{Type: "delta", Content: "future awaits"})
			writeStreamEvent(t, w, chatStreamEvent{
				Type: "done",
				Chat: chatData{ID: "c1", Role: "assistant", Content: "A bright future awaits"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			writeTestJSON(w, http.StatusOK, sessionListResponse{
				Message: "sessions retrieved",
				Data:    sessionListData{Sessions: []sessionData{{ID: "s1", Name: "Moving to a new city"}}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	c, err := newClient(server.URL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.creds.AccessToken = "test-access"

	m := newChatModel(c, meData{}, nil, nil, 80, 24)
	m.current = &sessionData{ID: "s1", Name: "Moving to a new city"}
	m.loading = true
	m.loadingText = "Jano is thinking..."

	ch := make(chan chatStreamMsg, 64)
	m.streamCh = ch

	_ = startChatStreamCmd(c, *m.current, "What next?", ch)()

	for i := 0; i < 20; i++ {
		msg := waitChatStreamCmd(ch)()
		if msg == nil {
			break
		}

		streamMsg, ok := msg.(chatStreamMsg)
		if !ok {
			t.Fatalf("unexpected message %T", msg)
		}

		m, _ = m.Update(streamMsg)

		if streamMsg.done {
			break
		}
	}

	if m.loading {
		t.Error("expected the loading state to end with the prediction")
	}
	if len(m.chats) != 1 || m.chats[0].Content != "A bright future awaits" {
		t.Fatalf("chats = %+v, want the stored prediction", m.chats)
	}
	if m.current == nil || m.current.Name != "Moving to a new city" {
		t.Errorf("current session = %+v, want the refreshed title", m.current)
	}
}
