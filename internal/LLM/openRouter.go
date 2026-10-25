package llm

import (
	"context"
	"fmt"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
)

func NewOpenRouterService(apiKey, modelName string) *LLM {
	return &LLM{
		apiKey: apiKey,
		model:  modelName,
	}
}

func (l *LLM) GenerateSessionName(content string) (string, error) {
	ctx := context.Background()

	client := openrouter.New(
		openrouter.WithSecurity(l.apiKey),
	)

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
