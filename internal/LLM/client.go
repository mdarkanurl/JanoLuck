package llm

import "time"

// DefaultRequestTimeout bounds a single non-streaming provider call. Streaming
// responses use the larger timeout of the streaming HTTP client.
const DefaultRequestTimeout = 2 * time.Minute

type LLM struct {
	apiKey     string
	model      string
	embedModel string
	timeout    time.Duration
}
