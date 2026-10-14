package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Handlers under test never reach the service layer when validation fails
// or when the request is unauthenticated, so a nil *Service is safe here.
func testHandler() *Handler {
	return AuthHandler(nil)
}

func doHandlerRequest(t *testing.T, fn http.HandlerFunc, method, body string, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "/", nil)
	} else {
		req = httptest.NewRequest(method, "/", strings.NewReader(body))
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	rr := httptest.NewRecorder()
	fn(rr, req)
	return rr
}

func decodeHandlerError(t *testing.T, rr *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var e ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e
}

func authContext() context.Context {
	ctx := context.WithValue(context.Background(), UserIDKey, "user-1")
	ctx = context.WithValue(ctx, EmailKey, "user@example.com")
	ctx = context.WithValue(ctx, TokenKey, "tok")
	return ctx
}

func TestSignUp_InvalidBody(t *testing.T) {
	h := testHandler()
	rr := doHandlerRequest(t, h.SignUp, http.MethodPost, `{invalid`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
	if got := decodeHandlerError(t, rr).Error; got != "invalid request body" {
		t.Errorf("error = %q, want %q", got, "invalid request body")
	}
}

func TestSignUp_InvalidInput(t *testing.T) {
	h := testHandler()

	tests := []struct {
		name string
		body string
	}{
		{name: "bad email", body: `{"email":"nope","password":"pass1234"}`},
		{name: "weak password no number", body: `{"email":"a@b.com","password":"passwordonly"}`},
		{name: "weak password no letter", body: `{"email":"a@b.com","password":"12345678"}`},
		{name: "short password", body: `{"email":"a@b.com","password":"a1"}`},
		{name: "missing fields", body: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := doHandlerRequest(t, h.SignUp, http.MethodPost, tt.body, nil)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rr.Code)
			}
			e := decodeHandlerError(t, rr)
			if e.Error != "invalid_input" {
				t.Errorf("error = %q, want %q", e.Error, "invalid_input")
			}
			if len(e.Fields) == 0 {
				t.Error("expected validation fields in body")
			}
		})
	}
}

func TestLogin_InvalidBodyAndInput(t *testing.T) {
	h := testHandler()

	rr := doHandlerRequest(t, h.Login, http.MethodPost, `{invalid`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed JSON: status = %d, want 400", rr.Code)
	}

	rr = doHandlerRequest(t, h.Login, http.MethodPost, `{"email":"bad","password":"x"}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid input: status = %d, want 400", rr.Code)
	}
	if got := decodeHandlerError(t, rr).Error; got != "invalid_input" {
		t.Errorf("error = %q, want invalid_input", got)
	}
}

func TestVerifyEmail_InvalidCode(t *testing.T) {
	h := testHandler()

	// Code below 100000 must be rejected by min/max validation.
	rr := doHandlerRequest(t, h.VerifyEmail, http.MethodPost, `{"email":"a@b.com","code":123}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	e := decodeHandlerError(t, rr)
	if e.Error != "invalid_input" {
		t.Errorf("error = %q, want invalid_input", e.Error)
	}

	// String-encoded codes are accepted by UnmarshalJSON; out-of-range still 400.
	rr = doHandlerRequest(t, h.VerifyEmail, http.MethodPost, `{"email":"a@b.com","code":"12"}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("string code out of range: status = %d, want 400", rr.Code)
	}

	// Malformed JSON.
	rr = doHandlerRequest(t, h.VerifyEmail, http.MethodPost, `not-json`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed JSON: status = %d, want 400", rr.Code)
	}
}

func TestRefresh_MissingToken(t *testing.T) {
	h := testHandler()
	rr := doHandlerRequest(t, h.Refresh, http.MethodPost, `{}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
	if got := decodeHandlerError(t, rr).Error; got != "invalid_input" {
		t.Errorf("error = %q, want invalid_input", got)
	}
}

func TestForgotPassword_InvalidEmail(t *testing.T) {
	h := testHandler()
	rr := doHandlerRequest(t, h.ForgotPassword, http.MethodPost, `{"email":"bad"}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestResetPassword_InvalidInput(t *testing.T) {
	h := testHandler()
	rr := doHandlerRequest(t, h.ResetPassword, http.MethodPost,
		`{"email":"a@b.com","code":111111,"newPassword":"weakpass"}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("weak new password: status = %d, want 400", rr.Code)
	}
}

func TestChangePassword_ValidationBeforeAuth(t *testing.T) {
	h := testHandler()

	// Invalid body is rejected even without auth context.
	rr := doHandlerRequest(t, h.ChangePassword, http.MethodPost, `{"currentPassword":"x"}`, nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}

	// Valid body but no auth context -> 401.
	rr = doHandlerRequest(t, h.ChangePassword, http.MethodPost,
		`{"currentPassword":"oldpass12","newPassword":"newpass12"}`, nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated: status = %d, want 401", rr.Code)
	}
}

func TestMe_Unauthenticated(t *testing.T) {
	h := testHandler()
	rr := doHandlerRequest(t, h.Me, http.MethodGet, "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestLogout_Unauthenticated(t *testing.T) {
	h := testHandler()
	rr := doHandlerRequest(t, h.Logout, http.MethodPost, "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestVerifyEmail_UppercaseEmailNormalized(t *testing.T) {
	// Handler normalizes email before validation, so an uppercase email
	// must normalize to lowercase and then pass struct validation.
	// (We assert the normalize+validate steps directly because the full
	// handler would proceed to the service/DB layer.)
	raw := "USER@EXAMPLE.COM"
	normalized := normalizeEmail(raw)
	if normalized != "user@example.com" {
		t.Fatalf("normalizeEmail(%q) = %q, want %q", raw, normalized, "user@example.com")
	}
	req := VerifyEmailRequest{Email: normalized, Code: 123456}
	if err := validate.Struct(req); err != nil {
		t.Errorf("normalized request should validate, got %v", err)
	}
}

func TestWriteError_Shapes(t *testing.T) {
	cases := []struct {
		name string
		msg  any
		want string
	}{
		{name: "string", msg: "boom", want: "boom"},
		{name: "error", msg: ErrUserExists, want: ErrUserExists.Error()},
		{name: "struct", msg: ErrorResponse{Error: "custom"}, want: "custom"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeError(rr, http.StatusBadRequest, tt.msg)
			if got := decodeHandlerError(t, rr).Error; got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
		})
	}
}
