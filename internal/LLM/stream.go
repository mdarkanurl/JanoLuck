package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

// openRouterBaseURL is the OpenRouter API root used for streaming requests.
// It is a variable so tests can point the streamer at a local server.
var openRouterBaseURL = defaultOpenRouterBaseURL

// openRouterHTTPClient performs the streaming requests.
var openRouterHTTPClient = &http.Client{Timeout: 5 * time.Minute}

// chatMessage is the wire format of a chat message sent to OpenRouter.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// streamChoice is a single choice inside a streamed chunk.
type streamChoice struct {
	Index int `json:"index"`
	Delta struct {
		Content *string `json:"content"`
		Role    *string `json:"role"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// streamChunk is a single server-sent event payload. OpenRouter sends the
// chunk fields at the top level, but some gateways nest them under "data".
type streamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Choices []streamChoice `json:"choices"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Data *streamChunk `json:"data"`
}

// unwrap returns the chunk holding the choices, following the optional
// "data" wrapper.
func (c *streamChunk) unwrap() *streamChunk {
	for c != nil && c.Data != nil && len(c.Choices) == 0 {
		c = c.Data
	}

	return c
}

// streamChatCompletion posts a streaming chat completion request and returns
// the concatenated completion. onChunk is called with every text fragment as
// soon as it arrives so callers can forward it to their own clients.
func (l *LLM) streamChatCompletion(ctx context.Context, messages []chatMessage, onChunk func(string) error) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"model":    l.model,
		"messages": messages,
		"stream":   true,
	})
	if err != nil {
		return "", fmt.Errorf("encode openrouter request: %w", err)
	}

	endpoint := strings.TrimRight(openRouterBaseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build openrouter request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+l.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "janoluck")

	resp, err := openRouterHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openrouter chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("openrouter chat request failed: %s", openRouterErrorMessage(raw, resp.StatusCode))
	}

	return readEventStream(resp.Body, onChunk)
}

// readEventStream consumes a server-sent event stream and collects the text
// deltas it carries.
func readEventStream(body io.Reader, onChunk func(string) error) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var prediction strings.Builder
	var payload strings.Builder

	handle := func() error {
		if payload.Len() == 0 {
			return nil
		}

		chunk := payload.String()
		payload.Reset()

		return applyStreamChunk(chunk, &prediction, onChunk)
	}

	for scanner.Scan() {
		line := scanner.Text()

		// A blank line terminates an event.
		if strings.TrimSpace(line) == "" {
			if err := handle(); err != nil {
				return prediction.String(), err
			}
			continue
		}

		// Comment lines keep the connection alive and carry no payload.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Event, id and retry fields are not used.
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimPrefix(data, " ")

		if payload.Len() > 0 {
			payload.WriteByte('\n')
		}
		payload.WriteString(data)
	}

	if err := scanner.Err(); err != nil {
		return prediction.String(), fmt.Errorf("read openrouter stream: %w", err)
	}

	// The stream may end without a trailing blank line.
	if err := handle(); err != nil {
		return prediction.String(), err
	}

	result := strings.TrimSpace(prediction.String())
	if result == "" {
		return "", fmt.Errorf("empty response from openrouter")
	}

	return result, nil
}

// applyStreamChunk extracts the text delta of a single event payload.
func applyStreamChunk(payload string, prediction *strings.Builder, onChunk func(string) error) error {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "[DONE]" {
		return nil
	}

	var chunk streamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return fmt.Errorf("decode openrouter stream chunk: %w", err)
	}

	unwrapped := chunk.unwrap()
	if unwrapped == nil {
		return nil
	}

	if unwrapped.Error != nil {
		message := strings.TrimSpace(unwrapped.Error.Message)
		if message == "" {
			message = "unknown error"
		}
		return fmt.Errorf("openrouter stream error: %s", message)
	}

	for _, choice := range unwrapped.Choices {
		if choice.Delta.Content == nil || *choice.Delta.Content == "" {
			continue
		}

		prediction.WriteString(*choice.Delta.Content)

		if onChunk != nil {
			if err := onChunk(*choice.Delta.Content); err != nil {
				return err
			}
		}
	}

	return nil
}

// openRouterErrorMessage extracts a readable message from an error response.
func openRouterErrorMessage(raw []byte, status int) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return strings.TrimSpace(payload.Error.Message)
	}

	message := strings.TrimSpace(string(raw))
	if message == "" {
		return http.StatusText(status)
	}

	return message
}
