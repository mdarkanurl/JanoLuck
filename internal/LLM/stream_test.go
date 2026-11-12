package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// useStreamServer points the streaming client at a local test server.
func useStreamServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	oldURL := openRouterBaseURL
	oldClient := openRouterHTTPClient

	openRouterBaseURL = server.URL
	openRouterHTTPClient = server.Client()

	t.Cleanup(func() {
		openRouterBaseURL = oldURL
		openRouterHTTPClient = oldClient
	})
}

func writeSSE(w http.ResponseWriter, frames ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, frame := range frames {
		_, _ = w.Write([]byte(frame))
	}
}

func chunkFrame(content string) string {
	return `data: {"id":"gen-1","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":{"content":"` + content + `","role":"assistant"},"finish_reason":null}]}` + "\n\n"
}

func TestGeneratePredictionStream(t *testing.T) {
	var requestBody string

	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q, want text/event-stream", got)
		}

		raw, _ := io.ReadAll(r.Body)
		requestBody = string(raw)

		writeSSE(w,
			chunkFrame("A bright "),
			chunkFrame("future "),
			chunkFrame("awaits"),
			"data: [DONE]\n\n",
		)
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	var deltas []string
	prediction, err := svc.GeneratePredictionStream(
		context.Background(),
		"What next?",
		[]string{"first message"},
		"Motivation",
		"Be uplifting.",
		func(chunk string) error {
			deltas = append(deltas, chunk)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("GeneratePredictionStream() error = %v", err)
	}

	if prediction != "A bright future awaits" {
		t.Errorf("prediction = %q, want %q", prediction, "A bright future awaits")
	}
	if strings.Join(deltas, "") != "A bright future awaits" {
		t.Errorf("deltas = %v, want the full prediction", deltas)
	}
	if len(deltas) != 3 {
		t.Errorf("got %d deltas, want 3", len(deltas))
	}

	if !strings.Contains(requestBody, `"stream":true`) {
		t.Errorf("request body missing stream flag: %s", requestBody)
	}
	if !strings.Contains(requestBody, "fortune teller") {
		t.Error("request body missing the system prompt")
	}
	if !strings.Contains(requestBody, "Motivation") || !strings.Contains(requestBody, "Be uplifting.") {
		t.Error("request body missing the active mode instructions")
	}
	if !strings.Contains(requestBody, "first message") {
		t.Error("request body missing the previous chat context")
	}
}

func TestGeneratePredictionStreamIgnoresComments(t *testing.T) {
	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w,
			": OPENROUTER PROCESSING\n\n",
			chunkFrame("stars "),
			": OPENROUTER PROCESSING\n\n",
			chunkFrame("ahead"),
			"data: [DONE]\n\n",
		)
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	prediction, err := svc.GeneratePredictionStream(context.Background(), "hello", nil, "", "", nil)
	if err != nil {
		t.Fatalf("GeneratePredictionStream() error = %v", err)
	}
	if prediction != "stars ahead" {
		t.Errorf("prediction = %q, want %q", prediction, "stars ahead")
	}
}

func TestGeneratePredictionStreamNestedChunk(t *testing.T) {
	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w,
			`data: {"data":{"id":"gen-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"nested"}}]}}`+"\n\n",
			"data: [DONE]\n\n",
		)
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	prediction, err := svc.GeneratePredictionStream(context.Background(), "hello", nil, "", "", nil)
	if err != nil {
		t.Fatalf("GeneratePredictionStream() error = %v", err)
	}
	if prediction != "nested" {
		t.Errorf("prediction = %q, want nested", prediction)
	}
}

func TestGeneratePredictionStreamMidStreamError(t *testing.T) {
	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w,
			chunkFrame("partial"),
			`data: {"id":"gen-1","object":"chat.completion.chunk","error":{"code":502,"message":"Provider disconnected unexpectedly"},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}`+"\n\n",
		)
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	_, err := svc.GeneratePredictionStream(context.Background(), "hello", nil, "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "Provider disconnected unexpectedly") {
		t.Errorf("error = %v, want the provider error", err)
	}
}

func TestGeneratePredictionStreamHTTPError(t *testing.T) {
	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid model specified","code":400}}`))
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	_, err := svc.GeneratePredictionStream(context.Background(), "hello", nil, "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "Invalid model specified") {
		t.Errorf("error = %v, want the openrouter error message", err)
	}
}

func TestGeneratePredictionStreamEmptyResponse(t *testing.T) {
	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, "data: [DONE]\n\n")
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	_, err := svc.GeneratePredictionStream(context.Background(), "hello", nil, "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "empty response from openrouter") {
		t.Errorf("error = %v, want empty response from openrouter", err)
	}
}

func TestGeneratePredictionStreamCallbackError(t *testing.T) {
	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w,
			chunkFrame("one"),
			chunkFrame("two"),
			"data: [DONE]\n\n",
		)
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	boom := errors.New("client gone")

	_, err := svc.GeneratePredictionStream(context.Background(), "hello", nil, "", "", func(string) error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the callback error", err)
	}
}

func TestGeneratePredictionStreamRespectsContext(t *testing.T) {
	useStreamServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, chunkFrame("hello"))
	})

	svc := NewOpenRouterService("test-key", "test-model", "embed-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.GeneratePredictionStream(ctx, "hello", nil, "", "", nil)
	if err == nil {
		t.Error("expected an error for a cancelled context")
	}
}
