package users

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func doProfileRequest(t *testing.T, fn http.HandlerFunc, method, body, userID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, "/profiles", strings.NewReader(body))
	if userID != "" {
		ctx := context.WithValue(req.Context(), auth.UserIDKey, userID)
		ctx = context.WithValue(ctx, auth.EmailKey, "user@example.com")
		req = req.WithContext(ctx)
	}

	rr := httptest.NewRecorder()
	fn(rr, req)
	return rr
}

func decodeProfileError(t *testing.T, rr *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var e ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e
}

func decodeProfileResponse(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rr.Body).Decode(v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func profileHandler(t *testing.T, db *testutil.DB) *Handler {
	t.Helper()
	return NewHandler(profileService(t, db))
}

func TestProfileWriteJSONAndError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusCreated, map[string]string{"ok": "yes"})

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	decodeProfileResponse(t, rr, &body)
	if body["ok"] != "yes" {
		t.Errorf("body = %v, want ok=yes", body)
	}

	cases := []struct {
		name string
		msg  any
		want string
	}{
		{name: "string", msg: "boom", want: "boom"},
		{name: "error", msg: ErrNotFound, want: ErrNotFound.Error()},
		{name: "struct", msg: ErrorResponse{Error: "custom"}, want: "custom"},
		{name: "other", msg: 42, want: "internal server error"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeError(rr, http.StatusBadRequest, tt.msg)
			if got := decodeProfileError(t, rr).Error; got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProfileValidationMessage(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "required",
			value: struct {
				Name string `validate:"required"`
			}{},
			want: "Name is required",
		},
		{
			name: "min string",
			value: struct {
				Short string `validate:"min=3"`
			}{Short: "ab"},
			want: "must be at least 3 characters",
		},
		{
			name: "max string",
			value: struct {
				Long string `validate:"max=3"`
			}{Long: "abcd"},
			want: "must be at most 3 characters",
		},
		{
			name: "min slice",
			value: struct {
				Items []string `validate:"min=2"`
			}{Items: []string{"one"}},
			want: "must contain at least 2 item(s)",
		},
		{
			name: "max slice",
			value: struct {
				Items []string `validate:"max=2"`
			}{Items: []string{"one", "two", "three"}},
			want: "must contain at most 2 item(s)",
		},
		{
			name: "min integer",
			value: struct {
				Count int `validate:"min=2"`
			}{Count: 1},
			want: "must be at least 2",
		},
		{
			name: "max integer",
			value: struct {
				Count int `validate:"max=2"`
			}{Count: 3},
			want: "must be at most 2",
		},
		{
			name: "default",
			value: struct {
				Email string `validate:"email"`
			}{Email: "bad"},
			want: "failed on 'email' validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.value)
			if err == nil {
				t.Fatal("expected validation error")
			}
			fields, ok := err.(validator.ValidationErrors)
			if !ok || len(fields) != 1 {
				t.Fatalf("unexpected errors: %v", err)
			}
			if got := validationMessage(fields[0]); got != tt.want {
				t.Errorf("validationMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateProfileHandlerValidation(t *testing.T) {
	h := NewHandler(nil)

	cases := []struct {
		name   string
		body   string
		userID string
		status int
	}{
		{name: "malformed body", body: `{`, userID: profileUserID.String(), status: http.StatusBadRequest},
		{name: "missing fields", body: `{}`, userID: profileUserID.String(), status: http.StatusBadRequest},
		{name: "unauthenticated", body: `{"name":"Ada","qOne":"Earth","qTwo":"Engineer"}`, userID: "", status: http.StatusUnauthorized},
		{name: "invalid user", body: `{"name":"Ada","qOne":"Earth","qTwo":"Engineer"}`, userID: "bad", status: http.StatusBadRequest},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := doProfileRequest(t, h.CreateProfile, http.MethodPost, tt.body, tt.userID)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
		})
	}

	rr := doProfileRequest(t, h.CreateProfile, http.MethodPost, `{}`, profileUserID.String())
	e := decodeProfileError(t, rr)
	if e.Error != "invalid_input" {
		t.Errorf("error = %q, want invalid_input", e.Error)
	}
	if len(e.Fields) == 0 {
		t.Error("expected validation fields")
	}
}

func TestCreateProfileHandlerServiceErrors(t *testing.T) {
	body := `{"name":"Ada","qOne":"Earth","qTwo":"Engineer"}`

	t.Run("already exists", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns, profileRowValues()), nil
			},
		})

		rr := doProfileRequest(t, h.CreateProfile, http.MethodPost, body, profileUserID.String())
		if rr.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409 (body %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		rr := doProfileRequest(t, h.CreateProfile, http.MethodPost, body, profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("create error", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.QueryError(sql.ErrNoRows)
				}
				return testutil.QueryError(errors.New("boom"))
			},
		})

		rr := doProfileRequest(t, h.CreateProfile, http.MethodPost, body, profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("corrupt questions", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.QueryError(sql.ErrNoRows)
				}
				return testutil.NewRows(profileColumns,
					[]driver.Value{profileID.String(), "Ada", []byte("{"), profileUserID.String()},
				), nil
			},
		})

		rr := doProfileRequest(t, h.CreateProfile, http.MethodPost, body, profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %s)", rr.Code, rr.Body.String())
		}
	})
}

func TestCreateProfileHandlerSuccess(t *testing.T) {
	h := profileHandler(t, &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "GetProfileByUserID") {
				return testutil.QueryError(sql.ErrNoRows)
			}
			return testutil.NewRows(profileColumns, profileRowValues()), nil
		},
	})

	rr := doProfileRequest(t, h.CreateProfile, http.MethodPost,
		`{"name":"Ada","qOne":"Earth","qTwo":"Engineer"}`, profileUserID.String())
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rr.Code, rr.Body.String())
	}

	var resp ProfileResponse
	decodeProfileResponse(t, rr, &resp)
	if resp.Message != "profile created" || resp.Data.ID != profileID.String() || resp.Data.UserID != profileUserID.String() {
		t.Errorf("unexpected response: %+v", resp)
	}
	if len(resp.Data.Questions) != 1 {
		t.Errorf("got %d questions, want 1", len(resp.Data.Questions))
	}
}

func TestGetProfileHandler(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		h := NewHandler(nil)
		rr := doProfileRequest(t, h.GetProfile, http.MethodGet, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("invalid user", func(t *testing.T) {
		h := NewHandler(nil)
		rr := doProfileRequest(t, h.GetProfile, http.MethodGet, "", "bad")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})
		rr := doProfileRequest(t, h.GetProfile, http.MethodGet, "", profileUserID.String())
		if rr.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rr.Code)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})
		rr := doProfileRequest(t, h.GetProfile, http.MethodGet, "", profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})

	t.Run("corrupt questions", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns,
					[]driver.Value{profileID.String(), "Ada", []byte("{"), profileUserID.String()},
				), nil
			},
		})
		rr := doProfileRequest(t, h.GetProfile, http.MethodGet, "", profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns, profileRowValues()), nil
			},
		})
		rr := doProfileRequest(t, h.GetProfile, http.MethodGet, "", profileUserID.String())
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
		}

		var resp ProfileResponse
		decodeProfileResponse(t, rr, &resp)
		if resp.Message != "profile retrieved" || resp.Data.Name != "Ada" {
			t.Errorf("unexpected response: %+v", resp)
		}
	})
}

func TestProfileExistsHandler(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		h := NewHandler(nil)
		rr := doProfileRequest(t, h.ProfileExists, http.MethodGet, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("invalid user", func(t *testing.T) {
		h := NewHandler(nil)
		rr := doProfileRequest(t, h.ProfileExists, http.MethodGet, "", "bad")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})
		rr := doProfileRequest(t, h.ProfileExists, http.MethodGet, "", profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			},
		})
		rr := doProfileRequest(t, h.ProfileExists, http.MethodGet, "", profileUserID.String())
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
		}

		var resp ProfileExistsResponse
		decodeProfileResponse(t, rr, &resp)
		if !resp.Data.Exists || resp.Data.UserID != profileUserID.String() {
			t.Errorf("unexpected response: %+v", resp)
		}
	})
}

func TestUpdateProfileHandlerValidation(t *testing.T) {
	h := NewHandler(nil)

	cases := []struct {
		name   string
		body   string
		userID string
		status int
	}{
		{name: "malformed body", body: `{`, userID: profileUserID.String(), status: http.StatusBadRequest},
		{name: "empty update", body: `{}`, userID: profileUserID.String(), status: http.StatusBadRequest},
		{name: "unauthenticated", body: `{"name":"Grace"}`, userID: "", status: http.StatusUnauthorized},
		{name: "invalid user", body: `{"name":"Grace"}`, userID: "bad", status: http.StatusBadRequest},
		{name: "name too long", body: `{"name":"` + strings.Repeat("a", 101) + `"}`, userID: profileUserID.String(), status: http.StatusBadRequest},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := doProfileRequest(t, h.UpdateProfile, http.MethodPut, tt.body, tt.userID)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
		})
	}
}

func TestUpdateProfileHandlerServiceErrors(t *testing.T) {
	body := `{"name":"Grace"}`

	t.Run("not found", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})
		rr := doProfileRequest(t, h.UpdateProfile, http.MethodPut, body, profileUserID.String())
		if rr.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rr.Code)
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})
		rr := doProfileRequest(t, h.UpdateProfile, http.MethodPut, body, profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})

	t.Run("invalid question index", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns, profileRowValues()), nil
			},
		})
		rr := doProfileRequest(t, h.UpdateProfile, http.MethodPut, `{"questions":[{"index":5,"answer":"Mars"}]}`, profileUserID.String())
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (body %s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("update error", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.NewRows(profileColumns, profileRowValues()), nil
				}
				return testutil.QueryError(errors.New("boom"))
			},
		})
		rr := doProfileRequest(t, h.UpdateProfile, http.MethodPut, body, profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})

	t.Run("corrupt questions", func(t *testing.T) {
		h := profileHandler(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.NewRows(profileColumns, profileRowValues()), nil
				}
				return testutil.NewRows(profileColumns,
					[]driver.Value{profileID.String(), "Grace", []byte("{"), profileUserID.String()},
				), nil
			},
		})
		rr := doProfileRequest(t, h.UpdateProfile, http.MethodPut, body, profileUserID.String())
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})
}

func TestUpdateProfileHandlerSuccess(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "name", body: `{"name":"Grace"}`},
		{name: "questions", body: `{"questions":[{"index":0,"answer":"Mars"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := profileHandler(t, &testutil.DB{
				Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
					if strings.Contains(query, "GetProfileByUserID") {
						return testutil.NewRows(profileColumns, profileRowValues()), nil
					}
					return testutil.NewRows(profileColumns, profileRowValues()), nil
				},
			})

			rr := doProfileRequest(t, h.UpdateProfile, http.MethodPut, tt.body, profileUserID.String())
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
			}
			var resp ProfileResponse
			decodeProfileResponse(t, rr, &resp)
			if resp.Message != "profile updated" {
				t.Errorf("message = %q", resp.Message)
			}
		})
	}
}
