package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openrouter "github.com/OpenRouterTeam/go-sdk"
)

func TestNewOpenRouterService(t *testing.T) {
	svc := NewOpenRouterService("key", "chat-model", "embed-model")
	if svc == nil {
		t.Fatal("NewOpenRouterService() returned nil")
	}
	if svc.apiKey != "key" || svc.model != "chat-model" || svc.embedModel != "embed-model" {
		t.Errorf("unexpected service fields: %+v", svc)
	}
}

func TestFormatPreviousChats(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{name: "nil", in: nil, want: ""},
		{name: "empty", in: []string{}, want: ""},
		{name: "values", in: []string{"one", "two"}, want: "- one\n- two"},
		{name: "trims and skips blanks", in: []string{"  one  ", "", "   ", "two"}, want: "- one\n- two"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPreviousChats(tt.in); got != tt.want {
				t.Errorf("formatPreviousChats() = %q, want %q", got, tt.want)
			}
		})
	}
}

func testLLM(t *testing.T, handler http.HandlerFunc) *LLM {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	old := newOpenRouterClient
	newOpenRouterClient = func(apiKey string) *openrouter.OpenRouter {
		return openrouter.New(
			openrouter.WithSecurity(apiKey),
			openrouter.WithServerURL(server.URL),
		)
	}
	t.Cleanup(func() { newOpenRouterClient = old })

	return NewOpenRouterService("test-key", "test-model", "embed-model")
}

func chatResponse(content string) string {
	body := map[string]any{
		"id":      "chat-1",
		"object":  "chat.completion",
		"created": 0,
		"model":   "test-model",
		"choices": []any{
			map[string]any{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
			},
		},
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func TestGenerateSessionName(t *testing.T) {
	var requestBody string

	svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		raw, _ := io.ReadAll(r.Body)
		requestBody = string(raw)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatResponse("The Hidden Path")))
	})

	name, err := svc.GenerateSessionName("Will I find love?")
	if err != nil {
		t.Fatalf("GenerateSessionName() error = %v", err)
	}
	if name != "The Hidden Path" {
		t.Errorf("name = %q, want The Hidden Path", name)
	}
	if !strings.Contains(requestBody, "Session Naming Agent") {
		t.Error("request body missing session naming prompt")
	}
	if !strings.Contains(requestBody, "Will I find love?") {
		t.Error("request body missing user content")
	}
}

func TestGeneratePrediction(t *testing.T) {
	var requestBody string

	svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requestBody = string(raw)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatResponse("A bright future awaits")))
	})

	prediction, err := svc.GeneratePrediction("What next?", []string{"first message", "second message"}, "Motivation", "Be uplifting.")
	if err != nil {
		t.Fatalf("GeneratePrediction() error = %v", err)
	}
	if prediction != "A bright future awaits" {
		t.Errorf("prediction = %q", prediction)
	}
	if !strings.Contains(requestBody, "- first message") || !strings.Contains(requestBody, "- second message") {
		t.Error("request body missing previous chat context")
	}
	if !strings.Contains(requestBody, "Motivation") || !strings.Contains(requestBody, "Be uplifting.") {
		t.Error("request body missing active mode instructions")
	}
}

func TestChatMethodsErrorResponses(t *testing.T) {
	svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request","type":"invalid_request_error"}}`))
	})

	if _, err := svc.GenerateSessionName("hello"); err == nil {
		t.Error("GenerateSessionName() expected error")
	}
	if _, err := svc.GeneratePrediction("hello", nil, "", ""); err == nil {
		t.Error("GeneratePrediction() expected error")
	}
}

func TestChatMethodsEmptyChoices(t *testing.T) {
	svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat-1","object":"chat.completion","model":"test-model","choices":[]}`))
	})

	if _, err := svc.GenerateSessionName("hello"); err == nil {
		t.Error("GenerateSessionName() expected error for empty choices")
	}
	if _, err := svc.GeneratePrediction("hello", nil, "", ""); err == nil {
		t.Error("GeneratePrediction() expected error for empty choices")
	}
}

func TestGenerateEmbedding(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var requestBody string
		svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/embeddings" {
				t.Errorf("path = %s, want /embeddings", r.URL.Path)
			}
			raw, _ := io.ReadAll(r.Body)
			requestBody = string(raw)

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"emb-1","object":"list","model":"embed-model","data":[{"index":0,"object":"embedding","embedding":[0.1,0.2,0.3]}]}`))
		})

		embedding, err := svc.GenerateEmbedding("hello")
		if err != nil {
			t.Fatalf("GenerateEmbedding() error = %v", err)
		}
		if len(embedding) != 3 || embedding[0] != 0.1 || embedding[2] != 0.3 {
			t.Errorf("embedding = %v, want [0.1 0.2 0.3]", embedding)
		}
		if !strings.Contains(requestBody, `"dimensions":1536`) {
			t.Error("request body missing 1536 dimensions")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"emb-1","object":"list","model":"embed-model","data":[]}`))
		})

		if _, err := svc.GenerateEmbedding("hello"); err == nil || !strings.Contains(err.Error(), "empty embeddings data") {
			t.Errorf("error = %v, want empty embeddings data", err)
		}
	})

	t.Run("base64 unsupported", func(t *testing.T) {
		svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"emb-1","object":"list","model":"embed-model","data":[{"index":0,"object":"embedding","embedding":"cGF5bG9hZA=="}]}`))
		})

		if _, err := svc.GenerateEmbedding("hello"); err == nil || !strings.Contains(err.Error(), "base64 embeddings not supported") {
			t.Errorf("error = %v, want base64 unsupported", err)
		}
	})

	t.Run("http error", func(t *testing.T) {
		svc := testLLM(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
		})

		if _, err := svc.GenerateEmbedding("hello"); err == nil {
			t.Error("expected error")
		}
	})
}
