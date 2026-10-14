package auth

import (
	"encoding/json"
	"testing"
)

func TestVerificationCode_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    VerificationCode
		wantErr bool
	}{
		{name: "json number", input: `{"code":123456}`, want: 123456},
		{name: "json string", input: `{"code":"123456"}`, want: 123456},
		{name: "json string with spaces", input: `{"code":" 123456 "}`, want: 123456},
		{name: "non-numeric string", input: `{"code":"abc"}`, wantErr: true},
		{name: "empty string", input: `{"code":""}`, wantErr: true},
		{name: "object", input: `{"code":{}}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req VerifyEmailRequest
			// Only Email is required for struct completeness in this unit;
			// we check the Code field unmarshalling behaviour.
			err := json.Unmarshal([]byte(tt.input), &req)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (code=%d)", req.Code)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.Code != tt.want {
				t.Errorf("code = %d, want %d", req.Code, tt.want)
			}
		})
	}
}

func TestSignUpRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     SignUpRequest
		wantErr bool
	}{
		{name: "valid", req: SignUpRequest{Email: "user@example.com", Password: "pass1234"}, wantErr: false},
		{name: "bad email", req: SignUpRequest{Email: "not-an-email", Password: "pass1234"}, wantErr: true},
		{name: "missing email", req: SignUpRequest{Email: "", Password: "pass1234"}, wantErr: true},
		{name: "short password", req: SignUpRequest{Email: "a@b.com", Password: "a1"}, wantErr: true},
		{name: "password without number", req: SignUpRequest{Email: "a@b.com", Password: "passwordonly"}, wantErr: true},
		{name: "password without letter", req: SignUpRequest{Email: "a@b.com", Password: "12345678"}, wantErr: true},
		{name: "password letters+numbers", req: SignUpRequest{Email: "a@b.com", Password: "abc12345"}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.req)
			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestVerifyEmailRequest_CodeRange(t *testing.T) {
	valid := VerifyEmailRequest{Email: "a@b.com", Code: 123456}
	if err := validate.Struct(valid); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}

	tooSmall := VerifyEmailRequest{Email: "a@b.com", Code: 999}
	if err := validate.Struct(tooSmall); err == nil {
		t.Error("code < 100000 should fail min validation")
	}

	tooLarge := VerifyEmailRequest{Email: "a@b.com", Code: 1000000}
	if err := validate.Struct(tooLarge); err == nil {
		t.Error("code > 999999 should fail max validation")
	}
}

func TestResetPasswordRequest_Validation(t *testing.T) {
	valid := ResetPasswordRequest{Email: "a@b.com", Code: 123456, NewPassword: "newpass1"}
	if err := validate.Struct(valid); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}

	weak := ResetPasswordRequest{Email: "a@b.com", Code: 123456, NewPassword: "weakpass"}
	if err := validate.Struct(weak); err == nil {
		t.Error("password without number should fail strong_password validation")
	}
}

func TestChangePasswordRequest_Validation(t *testing.T) {
	valid := ChangePasswordRequest{CurrentPassword: "oldpass1", NewPassword: "newpass1"}
	if err := validate.Struct(valid); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}

	weak := ChangePasswordRequest{CurrentPassword: "oldpass1", NewPassword: "nonumbers"}
	if err := validate.Struct(weak); err == nil {
		t.Error("weak new password should fail validation")
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercases both parts", input: "User@Example.COM", want: "user@example.com"},
		{name: "trims spaces", input: "  user@example.com  ", want: "user@example.com"},
		{name: "mixed case with spaces", input: "  USER@Example.com ", want: "user@example.com"},
		{name: "no @ returns lowered", input: "NOTANEMAIL", want: "notanemail"},
		{name: "empty", input: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeEmail(tt.input); got != tt.want {
				t.Errorf("normalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
