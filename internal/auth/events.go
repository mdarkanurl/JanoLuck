package auth

import "time"

const (
	UserRegisteredTopic      = "user.registered"
	UserPasswordChangedTopic = "user.password_changed"
	UserPasswordResetTopic   = "user.password_reset"
	LoginSucceededTopic      = "auth.login_succeeded"
	LogoutTopic              = "auth.logout"
	eventPublishTimeout      = 5 * time.Second
)

type UserEvent struct {
	UserID     string    `json:"user_id"`
	Email      string    `json:"email,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}
