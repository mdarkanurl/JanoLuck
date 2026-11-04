package users

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

var (
	profileUserID = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	profileID     = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	profileJSON   = []byte(`[{"index":0,"question":"Where are you from?","answer":"Earth"}]`)
)

var profileColumns = []string{"id", "name", "questions", "userId"}

func profileRowValues() []driver.Value {
	return []driver.Value{profileID.String(), "Ada", profileJSON, profileUserID.String()}
}

func testRepository(t *testing.T, db *testutil.DB) *Repository {
	t.Helper()
	return NewRepository(testutil.NewQueries(t, db))
}

func TestIsUniqueViolation(t *testing.T) {
	if !isUniqueViolation(&pq.Error{Code: "23505"}) {
		t.Error("pq unique violation should be detected")
	}
	if isUniqueViolation(&pq.Error{Code: "23503"}) {
		t.Error("other pq errors should not be treated as unique violations")
	}
	if !isUniqueViolation(errors.New("duplicate key value violates unique constraint")) {
		t.Error("duplicate text should be detected")
	}
	if !isUniqueViolation(errors.New("UNIQUE constraint failed")) {
		t.Error("unique text should be detected")
	}
	if isUniqueViolation(errors.New("connection refused")) {
		t.Error("unrelated errors should not be unique violations")
	}
}

func TestRepositoryCreateProfile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns, profileRowValues()), nil
			},
		})

		profile, err := repo.CreateProfile(context.Background(), profileUserID, "Ada", BuildFixedQuestions("Earth", "Engineer"))
		if err != nil {
			t.Fatalf("CreateProfile() error = %v", err)
		}
		if profile.ID != profileID || profile.Name != "Ada" || profile.Userid != profileUserID {
			t.Errorf("unexpected profile: %+v", profile)
		}
	})

	t.Run("unique violation", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(&pq.Error{Code: "23505"})
			},
		})

		_, err := repo.CreateProfile(context.Background(), profileUserID, "Ada", nil)
		if !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("got %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		_, err := repo.CreateProfile(context.Background(), profileUserID, "Ada", nil)
		if !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryGetProfileByUserID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns, profileRowValues()), nil
			},
		})

		profile, err := repo.GetProfileByUserID(context.Background(), profileUserID)
		if err != nil {
			t.Fatalf("GetProfileByUserID() error = %v", err)
		}
		if profile.ID != profileID {
			t.Errorf("got id %s, want %s", profile.ID, profileID)
		}
		if !json.Valid(profile.Questions) {
			t.Errorf("questions are not valid JSON: %s", profile.Questions)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		if _, err := repo.GetProfileByUserID(context.Background(), profileUserID); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.GetProfileByUserID(context.Background(), profileUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryUpdateProfile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				values := profileRowValues()
				values[1] = "Grace"
				return testutil.NewRows(profileColumns, values), nil
			},
		})

		profile, err := repo.UpdateProfile(context.Background(), profileUserID, "Grace", BuildFixedQuestions("Earth", "Engineer"))
		if err != nil {
			t.Fatalf("UpdateProfile() error = %v", err)
		}
		if profile.Name != "Grace" {
			t.Errorf("got name %q, want Grace", profile.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		if _, err := repo.UpdateProfile(context.Background(), profileUserID, "Grace", nil); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.UpdateProfile(context.Background(), profileUserID, "Grace", nil); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryProfileExists(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			},
		})

		exists, err := repo.ProfileExists(context.Background(), profileUserID)
		if err != nil {
			t.Fatalf("ProfileExists() error = %v", err)
		}
		if !exists {
			t.Error("expected profile to exist")
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.ProfileExists(context.Background(), profileUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}
