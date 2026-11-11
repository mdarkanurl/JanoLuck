package llm

import (
	"context"
	"fmt"
	"strings"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
)

var newOpenRouterClient = func(apiKey string) *openrouter.OpenRouter {
	return openrouter.New(openrouter.WithSecurity(apiKey))
}

func NewOpenRouterService(apiKey, modelName, embedModelName string) *LLM {
	return &LLM{
		apiKey:     apiKey,
		model:      modelName,
		embedModel: embedModelName,
	}
}

func (l *LLM) GenerateSessionName(content string) (string, error) {
	return l.GenerateSessionNameContext(context.Background(), content)
}

func (l *LLM) GenerateSessionNameContext(ctx context.Context, content string) (string, error) {
	client := newOpenRouterClient(l.apiKey)

	res, err := client.Chat.Send(ctx, components.ChatRequest{
		Model: openrouter.Pointer(l.model),
		Messages: []components.ChatMessages{
			components.CreateChatMessagesSystem(
				components.ChatSystemMessage{
					Role: components.ChatSystemMessageRoleSystem,
					Content: components.CreateChatSystemMessageContentStr(
						`You are a Session Naming Agent.

Your only job is to generate a short, catchy, and intriguing session title based on the user’s first message.

### Rules
- Output **only** the session name. No explanations, no quotes, no extra text.
- Keep the name between 3–7 words.
- Make it playful, mystical, or slightly dramatic — matching the fun “predict my future” vibe of the app.
- Capture the core topic or question from the user’s message.
- Prefer titles that feel like chapter names, tarot card titles, or fortune-cookie headers.
- Avoid generic titles like “Future Prediction” or “User Question”.
- Do not include the user’s name unless it is highly relevant and adds charm.
- Never use hashtags, emojis, or punctuation at the end (no periods or question marks).

### Style examples
- User asks about love life → “The Heart’s Hidden Roadmap”
- User asks about career → “Climbing the Invisible Ladder”
- User asks about a big decision → “The Fork in the Cosmic Path”
- User asks about money → “Whispers of the Golden Tide”
- User asks something vague → “Echoes from Tomorrow”

Generate the perfect session name now.`,
					),
				},
			),
			components.CreateChatMessagesUser(
				components.ChatUserMessage{
					Role:    components.ChatUserMessageRoleUser,
					Content: components.CreateChatUserMessageContentStr(content),
				},
			),
		},
	}, nil)

	if err != nil {
		return "", fmt.Errorf("openrouter chat request failed: %w", err)
	}

	if res == nil || res.ChatResult == nil || len(res.ChatResult.Choices) == 0 {
		return "", fmt.Errorf("empty response from openrouter")
	}

	msg := res.ChatResult.Choices[0].Message

	if content, ok := msg.Content.Get(); ok && content.Str != nil {
		return *content.Str, nil
	}

	return "", fmt.Errorf("could not extract session name from response")
}

func (l *LLM) GeneratePrediction(content string, previousChats []string, modeName, modePrompt string) (string, error) {
	ctx := context.Background()

	client := newOpenRouterClient(l.apiKey)

	systemPrompt := `You are a fortune teller. Generate a short, mystical prediction based on the user's message. Output only the prediction.`

	if modePrompt = strings.TrimSpace(modePrompt); modePrompt != "" {
		if modeName = strings.TrimSpace(modeName); modeName == "" {
			modeName = "custom"
		}
		systemPrompt += fmt.Sprintf(`

The user has selected the %q mode. Follow the mode instructions below while writing the prediction. They take priority over your default tone, but never override the need to keep the response safe, respectful, and clearly framed as reflection rather than guaranteed fact.

Mode instructions:
%s`, modeName, modePrompt)
	}

	if previousContext := formatPreviousChats(previousChats); previousContext != "" {
		systemPrompt += `

Here are the user's previous messages and predictions, ordered from most to least relevant. Use them as context to keep your prediction consistent and personal, but do not repeat them verbatim:

` + previousContext
	}

	res, err := client.Chat.Send(ctx, components.ChatRequest{
		Model: openrouter.Pointer(l.model),
		Messages: []components.ChatMessages{
			components.CreateChatMessagesSystem(
				components.ChatSystemMessage{
					Role: components.ChatSystemMessageRoleSystem,
					Content: components.CreateChatSystemMessageContentStr(
						systemPrompt,
					),
				},
			),
			components.CreateChatMessagesUser(
				components.ChatUserMessage{
					Role:    components.ChatUserMessageRoleUser,
					Content: components.CreateChatUserMessageContentStr(content),
				},
			),
		},
	}, nil)

	if err != nil {
		return "", fmt.Errorf("openrouter chat request failed: %w", err)
	}

	if res == nil || res.ChatResult == nil || len(res.ChatResult.Choices) == 0 {
		return "", fmt.Errorf("empty response from openrouter")
	}

	msg := res.ChatResult.Choices[0].Message

	if content, ok := msg.Content.Get(); ok && content.Str != nil {
		return *content.Str, nil
	}

	return "", fmt.Errorf("could not extract prediction from response")
}

func formatPreviousChats(previousChats []string) string {
	var sb strings.Builder
	for _, chat := range previousChats {
		chat = strings.TrimSpace(chat)
		if chat == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(chat)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

func (l *LLM) GenerateEmbedding(content string) ([]float64, error) {
	ctx := context.Background()

	client := newOpenRouterClient(l.apiKey)

	dimensions := int64(1536)
	res, err := client.Embeddings.Generate(ctx, operations.CreateEmbeddingsRequest{
		Model:      l.embedModel,
		Input:      operations.CreateInputUnionStr(content),
		Dimensions: &dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("openrouter embeddings request failed: %w", err)
	}

	if res == nil {
		return nil, fmt.Errorf("empty response from openrouter")
	}

	switch res.Type {
	case operations.CreateEmbeddingsResponseTypeCreateEmbeddingsResponseBody:
		if res.CreateEmbeddingsResponseBody == nil {
			return nil, fmt.Errorf("empty embeddings response body")
		}
		data := res.CreateEmbeddingsResponseBody.Data
		if len(data) == 0 {
			return nil, fmt.Errorf("empty embeddings data")
		}
		emb := data[0].Embedding
		if emb.ArrayOfNumber != nil {
			return emb.ArrayOfNumber, nil
		}
		if emb.Str != nil && *emb.Str != "" {
			return nil, fmt.Errorf("base64 embeddings not supported")
		}
		return nil, fmt.Errorf("could not extract embedding from response")
	case operations.CreateEmbeddingsResponseTypeStr:
		return nil, fmt.Errorf("unexpected string response from embeddings endpoint")
	}

	return nil, fmt.Errorf("could not extract embedding from response")
}
