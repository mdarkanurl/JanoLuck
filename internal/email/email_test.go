package email

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/resend/resend-go/v4"
)

func TestNewResendService(t *testing.T) {
	svc := NewResendService("test-key")
	if svc == nil {
		t.Fatal("NewResendService() returned nil")
	}
	if svc.resnedAPIKey != "test-key" {
		t.Errorf("api key = %q, want test-key", svc.resnedAPIKey)
	}
}

func TestSend(t *testing.T) {
	var gotAuth string
	var gotRequest resend.SendEmailRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/emails" {
			t.Errorf("path = %s, want /emails", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Errorf("decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resend.SendEmailResponse{Id: "email-123"})
	}))
	t.Cleanup(server.Close)

	old := newResendClient
	newResendClient = func(apiKey string) *resend.Client {
		client := resend.NewCustomClient(server.Client(), apiKey)
		base, err := url.Parse(server.URL + "/")
		if err != nil {
			t.Fatalf("parse test server url: %v", err)
		}
		client.BaseURL = base
		return client
	}
	t.Cleanup(func() { newResendClient = old })

	svc := NewResendService("test-key")
	if err := svc.Send("to@example.com", "Hello", "<p>Body</p>"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotRequest.From != "JunoLuck <JunoLuck@resend.dev>" {
		t.Errorf("From = %q", gotRequest.From)
	}
	if len(gotRequest.To) != 1 || gotRequest.To[0] != "to@example.com" {
		t.Errorf("To = %v, want [to@example.com]", gotRequest.To)
	}
	if gotRequest.Subject != "Hello" {
		t.Errorf("Subject = %q, want Hello", gotRequest.Subject)
	}
	if gotRequest.Html != "<p>Body</p>" {
		t.Errorf("Html = %q, want <p>Body</p>", gotRequest.Html)
	}
}
