package sessions

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func TestSessionParseUUID(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if got, err := parseUUID("  "+id.String()+"  ", "session id"); err != nil || got != id {
		t.Errorf("parseUUID() = (%s, %v), want (%s, nil)", got, err, id)
	}

	for _, raw := range []string{"", "nope", uuid.Nil.String()} {
		if _, err := parseUUID(raw, "session id"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("parseUUID(%q) error = %v, want ErrInvalidInput", raw, err)
		}
	}
}

func TestServiceCreateSessionInvalidUser(t *testing.T) {
	svc := NewService(nil, nil)

	if _, err := svc.CreateSession(context.Background(), "bad", "hello"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("got %v, want ErrInvalidInput", err)
	}
}

func TestServiceListSessionsRepositoryErrors(t *testing.T) {
	t.Run("count", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		}), nil)

		if _, err := svc.ListSessions(context.Background(), sessionUserID.String(), 1, 20); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "CountSessionsByUserID") {
					return testutil.NewRows([]string{"count"}, []driver.Value{int64(1)}), nil
				}
				return testutil.QueryError(errors.New("boom"))
			},
		}), nil)

		if _, err := svc.ListSessions(context.Background(), sessionUserID.String(), 1, 20); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestServiceListSessionsSuccess(t *testing.T) {
	svc := NewService(testRepository(t, &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "CountSessionsByUserID") {
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(5)}), nil
			}
			return testutil.NewRows(sessionColumns, sessionRowValues(), sessionRowValues()), nil
		},
	}), nil)

	result, err := svc.ListSessions(context.Background(), sessionUserID.String(), 1, 2)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if result.Total != 5 {
		t.Errorf("Total = %d, want 5", result.Total)
	}
	if len(result.Sessions) != 2 {
		t.Errorf("got %d sessions, want 2", len(result.Sessions))
	}
}

func TestServiceGetSessionValidation(t *testing.T) {
	svc := NewService(nil, nil)

	if _, err := svc.GetSession(context.Background(), "bad", sessionID.String()); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.GetSession(context.Background(), sessionUserID.String(), "bad"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad session: got %v, want ErrInvalidInput", err)
	}
}

func TestServiceGetSessionSuccessAndErrors(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(sessionColumns, sessionRowValues()), nil
			},
		}), nil)

		session, err := svc.GetSession(context.Background(), sessionUserID.String(), sessionID.String())
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}
		if session.ID != sessionID {
			t.Errorf("got id %s, want %s", session.ID, sessionID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		}), nil)

		if _, err := svc.GetSession(context.Background(), sessionUserID.String(), sessionID.String()); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		}), nil)

		if _, err := svc.GetSession(context.Background(), sessionUserID.String(), sessionID.String()); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestServiceDeleteSessionValidation(t *testing.T) {
	svc := NewService(nil, nil)

	if err := svc.DeleteSession(context.Background(), "bad", sessionID.String()); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user: got %v, want ErrInvalidInput", err)
	}
	if err := svc.DeleteSession(context.Background(), sessionUserID.String(), "bad"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad session: got %v, want ErrInvalidInput", err)
	}
}

func TestServiceDeleteSessionSuccessAndErrors(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(1)
			},
		}), nil)

		if err := svc.DeleteSession(context.Background(), sessionUserID.String(), sessionID.String()); err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(0)
			},
		}), nil)

		if err := svc.DeleteSession(context.Background(), sessionUserID.String(), sessionID.String()); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.ExecError(errors.New("boom"))
			},
		}), nil)

		if err := svc.DeleteSession(context.Background(), sessionUserID.String(), sessionID.String()); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}
