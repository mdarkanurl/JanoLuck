package sessions

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

var (
	sessionUserID  = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	sessionID      = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	sessionTime    = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sessionColumns = []string{"id", "name", "userId", "create_at", "update_at"}
)

func sessionRowValues() []driver.Value {
	return []driver.Value{
		sessionID.String(),
		"My Session",
		sessionUserID.String(),
		sessionTime,
		sessionTime,
	}
}

func testRepository(t *testing.T, db *testutil.DB) *Repository {
	t.Helper()
	return NewRepository(testutil.NewQueries(t, db))
}

func TestRepositoryCreateSession(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(sessionColumns, sessionRowValues()), nil
			},
		})

		session, err := repo.CreateSession(context.Background(), sessionUserID, "My Session")
		if err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}
		if session.ID != sessionID || session.Name != "My Session" || session.Userid != sessionUserID {
			t.Errorf("unexpected session: %+v", session)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.CreateSession(context.Background(), sessionUserID, "My Session"); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryListSessionsByUserID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(sessionColumns, sessionRowValues(), sessionRowValues()), nil
			},
		})

		sessions, err := repo.ListSessionsByUserID(context.Background(), sessionUserID, 10, 0)
		if err != nil {
			t.Fatalf("ListSessionsByUserID() error = %v", err)
		}
		if len(sessions) != 2 {
			t.Fatalf("got %d sessions, want 2", len(sessions))
		}
	})

	t.Run("nil result becomes empty slice", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(sessionColumns), nil
			},
		})

		sessions, err := repo.ListSessionsByUserID(context.Background(), sessionUserID, 10, 0)
		if err != nil {
			t.Fatalf("ListSessionsByUserID() error = %v", err)
		}
		if sessions == nil || len(sessions) != 0 {
			t.Errorf("got %#v, want empty non-nil slice", sessions)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.ListSessionsByUserID(context.Background(), sessionUserID, 10, 0); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryCountSessionsByUserID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(4)}), nil
			},
		})

		total, err := repo.CountSessionsByUserID(context.Background(), sessionUserID)
		if err != nil {
			t.Fatalf("CountSessionsByUserID() error = %v", err)
		}
		if total != 4 {
			t.Errorf("got %d, want 4", total)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.CountSessionsByUserID(context.Background(), sessionUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryGetSessionByID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(sessionColumns, sessionRowValues()), nil
			},
		})

		session, err := repo.GetSessionByID(context.Background(), sessionID, sessionUserID)
		if err != nil {
			t.Fatalf("GetSessionByID() error = %v", err)
		}
		if session.ID != sessionID {
			t.Errorf("got id %s, want %s", session.ID, sessionID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		if _, err := repo.GetSessionByID(context.Background(), sessionID, sessionUserID); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.GetSessionByID(context.Background(), sessionID, sessionUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryDeleteSession(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(1)
			},
		})

		if err := repo.DeleteSession(context.Background(), sessionID, sessionUserID); err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(0)
			},
		})

		if err := repo.DeleteSession(context.Background(), sessionID, sessionUserID); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.ExecError(errors.New("boom"))
			},
		})

		if err := repo.DeleteSession(context.Background(), sessionID, sessionUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryUpdateSessionName(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(1)
			},
		})

		if err := repo.UpdateSessionName(context.Background(), sessionID, sessionUserID, "New Name"); err != nil {
			t.Fatalf("UpdateSessionName() error = %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(0)
			},
		})

		if err := repo.UpdateSessionName(context.Background(), sessionID, sessionUserID, "New Name"); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.ExecError(errors.New("boom"))
			},
		})

		if err := repo.UpdateSessionName(context.Background(), sessionID, sessionUserID, "New Name"); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}
