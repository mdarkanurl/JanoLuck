package email

const (
	VerificationTopic  = "email.verification"
	PasswordResetTopic = "email.password_reset"
)

type Job struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}
