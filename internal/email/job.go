package email

const VerificationTopic = "email.verification"

type Job struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}
