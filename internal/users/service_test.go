package users

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func profileRowWithQuestions(t *testing.T, questions []Question) []driver.Value {
	t.Helper()
	raw, err := json.Marshal(questions)
	if err != nil {
		t.Fatalf("marshal questions: %v", err)
	}
	return []driver.Value{profileID.String(), "Ada", raw, profileUserID.String()}
}

func profileService(t *testing.T, db *testutil.DB) *Service {
	t.Helper()
	return NewService(testRepository(t, db))
}

func TestBuildFixedQuestions(t *testing.T) {
	questions := BuildFixedQuestions("Earth", "Engineer")

	if len(questions) != len(FixedQuestions) {
		t.Fatalf("got %d questions, want %d", len(questions), len(FixedQuestions))
	}
	for i, q := range questions {
		if q.Index != i {
			t.Errorf("question %d index = %d, want %d", i, q.Index, i)
		}
		if q.Question != FixedQuestions[i] {
			t.Errorf("question %d = %q, want %q", i, q.Question, FixedQuestions[i])
		}
	}
	if questions[0].Answer != "Earth" || questions[1].Answer != "Engineer" {
		t.Errorf("unexpected answers: %+v", questions)
	}
}

func TestQuestionAnswerUpdateQuestionIndex(t *testing.T) {
	index := 2
	id := 3

	if got, ok := (QuestionAnswerUpdate{Index: &index}).QuestionIndex(); !ok || *got != index {
		t.Errorf("index should win, got (%v, %v)", got, ok)
	}
	if got, ok := (QuestionAnswerUpdate{ID: &id}).QuestionIndex(); !ok || *got != id {
		t.Errorf("id fallback failed, got (%v, %v)", got, ok)
	}
	if _, ok := (QuestionAnswerUpdate{}).QuestionIndex(); ok {
		t.Error("empty update should not yield an index")
	}
}

func TestServiceCreateProfileInvalidUser(t *testing.T) {
	svc := NewService(nil)

	for _, id := range []string{"", "bad", "00000000-0000-0000-0000-000000000000"} {
		if _, err := svc.CreateProfile(context.Background(), id, "Ada", "Earth", "Engineer"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("CreateProfile(%q) error = %v, want ErrInvalidInput", id, err)
		}
	}
}

func TestServiceCreateProfileAlreadyExists(t *testing.T) {
	svc := profileService(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.NewRows(profileColumns, profileRowValues()), nil
		},
	})

	if _, err := svc.CreateProfile(context.Background(), profileUserID.String(), "Ada", "Earth", "Engineer"); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("got %v, want ErrAlreadyExists", err)
	}
}

func TestServiceCreateProfileLookupError(t *testing.T) {
	svc := profileService(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.QueryError(errors.New("boom"))
		},
	})

	if _, err := svc.CreateProfile(context.Background(), profileUserID.String(), "Ada", "Earth", "Engineer"); !errors.Is(err, ErrInternal) {
		t.Errorf("got %v, want ErrInternal", err)
	}
}

func TestServiceCreateProfileSuccess(t *testing.T) {
	svc := profileService(t, &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "GetProfileByUserID") {
				return testutil.QueryError(sql.ErrNoRows)
			}
			return testutil.NewRows(profileColumns, profileRowValues()), nil
		},
	})

	profile, err := svc.CreateProfile(context.Background(), profileUserID.String(), "Ada", "Earth", "Engineer")
	if err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}
	if profile.ID != profileID {
		t.Errorf("got id %s, want %s", profile.ID, profileID)
	}
}

func TestServiceCreateProfileCreateErrors(t *testing.T) {
	t.Run("unique violation", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.QueryError(sql.ErrNoRows)
				}
				return testutil.QueryError(&pq.Error{Code: "23505"})
			},
		})

		if _, err := svc.CreateProfile(context.Background(), profileUserID.String(), "Ada", "Earth", "Engineer"); !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("got %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("generic", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.QueryError(sql.ErrNoRows)
				}
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := svc.CreateProfile(context.Background(), profileUserID.String(), "Ada", "Earth", "Engineer"); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestServiceGetProfile(t *testing.T) {
	t.Run("invalid user", func(t *testing.T) {
		svc := NewService(nil)
		if _, err := svc.GetProfile(context.Background(), "bad"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("got %v, want ErrInvalidInput", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns, profileRowValues()), nil
			},
		})

		profile, err := svc.GetProfile(context.Background(), profileUserID.String())
		if err != nil {
			t.Fatalf("GetProfile() error = %v", err)
		}
		if profile.Name != "Ada" {
			t.Errorf("got name %q, want Ada", profile.Name)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		if _, err := svc.GetProfile(context.Background(), profileUserID.String()); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := svc.GetProfile(context.Background(), profileUserID.String()); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestServiceProfileExists(t *testing.T) {
	t.Run("invalid user", func(t *testing.T) {
		svc := NewService(nil)
		if _, err := svc.ProfileExists(context.Background(), "bad"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("got %v, want ErrInvalidInput", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			},
		})

		exists, err := svc.ProfileExists(context.Background(), profileUserID.String())
		if err != nil || !exists {
			t.Errorf("ProfileExists() = (%v, %v), want (true, nil)", exists, err)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := svc.ProfileExists(context.Background(), profileUserID.String()); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestServiceUpdateProfileInvalidInput(t *testing.T) {
	svc := NewService(nil)

	if _, err := svc.UpdateProfile(context.Background(), "bad", UpdateProfileRequest{Name: ptr("Ada")}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty update: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{Name: ptr("   ")}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("blank name: got %v, want ErrInvalidInput", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestServiceUpdateProfileLookupErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		if _, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{Name: ptr("Ada")}); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("repository error", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{Name: ptr("Ada")}); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})

	t.Run("corrupt questions", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns,
					[]driver.Value{profileID.String(), "Ada", []byte("{"), profileUserID.String()},
				), nil
			},
		})

		if _, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{Name: ptr("Ada")}); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestServiceUpdateProfileNameSuccess(t *testing.T) {
	svc := profileService(t, &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "GetProfileByUserID") {
				return testutil.NewRows(profileColumns, profileRowValues()), nil
			}
			values := profileRowValues()
			values[1] = "Grace"
			return testutil.NewRows(profileColumns, values), nil
		},
	})

	profile, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{Name: ptr("  Grace  ")})
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if profile.Name != "Grace" {
		t.Errorf("got name %q, want Grace", profile.Name)
	}
}

func TestServiceUpdateProfileQuestionsSuccess(t *testing.T) {
	existing := BuildFixedQuestions("Earth", "Engineer")

	tests := []struct {
		name   string
		update QuestionAnswerUpdate
	}{
		{name: "index", update: QuestionAnswerUpdate{Index: ptr(0), Answer: ptr("Mars")}},
		{name: "id alias", update: QuestionAnswerUpdate{ID: ptr(0), Answer: ptr("Mars")}},
		{name: "matching index and id", update: QuestionAnswerUpdate{Index: ptr(0), ID: ptr(0), Answer: ptr("Mars")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := profileService(t, &testutil.DB{
				Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
					if strings.Contains(query, "GetProfileByUserID") {
						return testutil.NewRows(profileColumns, profileRowWithQuestions(t, existing)), nil
					}
					updated := BuildFixedQuestions("Mars", "Engineer")
					return testutil.NewRows(profileColumns, profileRowWithQuestions(t, updated)), nil
				},
			})

			profile, err := svc.UpdateProfile(context.Background(), profileUserID.String(),
				UpdateProfileRequest{Questions: []QuestionAnswerUpdate{tt.update}})
			if err != nil {
				t.Fatalf("UpdateProfile() error = %v", err)
			}
			var questions []Question
			if err := json.Unmarshal(profile.Questions, &questions); err != nil {
				t.Fatalf("unmarshal questions: %v", err)
			}
			if questions[0].Answer != "Mars" {
				t.Errorf("got answer %q, want Mars", questions[0].Answer)
			}
		})
	}
}

func TestServiceUpdateProfileQuestionValidation(t *testing.T) {
	existing := BuildFixedQuestions("Earth", "Engineer")
	tests := []struct {
		name   string
		update QuestionAnswerUpdate
	}{
		{name: "missing index and id", update: QuestionAnswerUpdate{Answer: ptr("Mars")}},
		{name: "index and id mismatch", update: QuestionAnswerUpdate{Index: ptr(0), ID: ptr(1), Answer: ptr("Mars")}},
		{name: "missing answer", update: QuestionAnswerUpdate{Index: ptr(0)}},
		{name: "blank answer", update: QuestionAnswerUpdate{Index: ptr(0), Answer: ptr("  ")}},
		{name: "negative index", update: QuestionAnswerUpdate{Index: ptr(-1), Answer: ptr("Mars")}},
		{name: "index too large", update: QuestionAnswerUpdate{Index: ptr(2), Answer: ptr("Mars")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := profileService(t, &testutil.DB{
				Query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return testutil.NewRows(profileColumns, profileRowWithQuestions(t, existing)), nil
				},
			})

			_, err := svc.UpdateProfile(context.Background(), profileUserID.String(),
				UpdateProfileRequest{Questions: []QuestionAnswerUpdate{tt.update}})
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("got %v, want ErrInvalidInput", err)
			}
		})
	}

	t.Run("duplicate index", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(profileColumns, profileRowWithQuestions(t, existing)), nil
			},
		})

		_, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{
			Questions: []QuestionAnswerUpdate{
				{Index: ptr(0), Answer: ptr("Mars")},
				{Index: ptr(0), Answer: ptr("Venus")},
			},
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("got %v, want ErrInvalidInput", err)
		}
	})
}

func TestServiceUpdateProfileRepositoryErrors(t *testing.T) {
	existing := BuildFixedQuestions("Earth", "Engineer")

	t.Run("not found", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.NewRows(profileColumns, profileRowWithQuestions(t, existing)), nil
				}
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		_, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{Name: ptr("Grace")})
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("generic", func(t *testing.T) {
		svc := profileService(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "GetProfileByUserID") {
					return testutil.NewRows(profileColumns, profileRowWithQuestions(t, existing)), nil
				}
				return testutil.QueryError(errors.New("boom"))
			},
		})

		_, err := svc.UpdateProfile(context.Background(), profileUserID.String(), UpdateProfileRequest{Name: ptr("Grace")})
		if !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}
