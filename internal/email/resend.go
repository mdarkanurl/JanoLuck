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

	sent, err := client.Emails.Send(params)
	if err != nil {
		fmt.Println(err.Error())
	}
	fmt.Println(sent.Id)
	return nil
}
