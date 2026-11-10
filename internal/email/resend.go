package email

import (
	"fmt"

	"github.com/resend/resend-go/v4"
)

var newResendClient = resend.NewClient

func NewResendService(apiKey string) *Email {
	return &Email{
		resnedAPIKey: apiKey,
	}
}

func (r *Email) Send(
	to string,
	subject string,
	body string,
) error {
	client := newResendClient(r.resnedAPIKey)

	params := &resend.SendEmailRequest{
		From:    "JunoLuck <JunoLuck@resend.dev>",
		To:      []string{to},
		Html:    body,
		Subject: subject,
	}

	if _, err := client.Emails.Send(params); err != nil {
		return fmt.Errorf("resend: send email: %w", err)
	}

	return nil
}
