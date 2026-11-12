package topics

import (
	"github.com/mdarkanurl/JanoLuck/internal/analytics"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/email"
	"github.com/mdarkanurl/JanoLuck/internal/embeddings"
	"github.com/mdarkanurl/JanoLuck/internal/kafka"
	"github.com/mdarkanurl/JanoLuck/internal/moderation"
	"github.com/mdarkanurl/JanoLuck/internal/sessions"
)

func All() []kafka.Topic {
	return []kafka.Topic{
		{Name: email.VerificationTopic},
		{Name: email.PasswordResetTopic},
		{Name: email.NotificationTopic},
		{Name: sessions.TitleTopic},
		{Name: sessions.SummaryTopic},
		{Name: analytics.Topic},
		{Name: embeddings.Topic},
		{Name: moderation.Topic},
		{Name: auth.UserRegisteredTopic},
		{Name: auth.LoginSucceededTopic},
		{Name: auth.UserPasswordResetTopic},
		{Name: auth.UserPasswordChangedTopic},
		{Name: auth.LogoutTopic},
	}
}
