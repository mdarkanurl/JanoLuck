package users

var FixedQuestions = []string{
	"Where are you from?",
	"What do you do for living?",
}

type Question struct {
	Index    int    `json:"index"    validate:"min=0"`
	Question string `json:"question" validate:"required,min=1,max=1000"`
	Answer   string `json:"answer"   validate:"required,min=1,max=2000"`
}

type CreateProfileRequest struct {
	Name string `json:"name"  validate:"required,min=1,max=100"`
	QOne string `json:"qOne"  validate:"required,min=1,max=2000"`
	QTwo string `json:"qTwo"  validate:"required,min=1,max=2000"`
}

type QuestionAnswerUpdate struct {
	Index  *int    `json:"index,omitempty"  validate:"omitempty,min=0"`
	ID     *int    `json:"id,omitempty"     validate:"omitempty,min=0"`
	Answer *string `json:"answer"           validate:"required,min=1,max=2000"`
}

func (q QuestionAnswerUpdate) QuestionIndex() (*int, bool) {
	if q.Index != nil {
		return q.Index, true
	}
	if q.ID != nil {
		return q.ID, true
	}
	return nil, false
}

type UpdateProfileRequest struct {
	Name      *string                `json:"name,omitempty"      validate:"omitempty,min=1,max=100"`
	Questions []QuestionAnswerUpdate `json:"questions,omitempty" validate:"omitempty,dive"`
}

func BuildFixedQuestions(qOne, qTwo string) []Question {
	answers := []string{qOne, qTwo}
	questions := make([]Question, len(FixedQuestions))
	for i, q := range FixedQuestions {
		questions[i] = Question{Index: i, Question: q, Answer: answers[i]}
	}
	return questions
}

type ProfileData struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Questions []Question `json:"questions"`
	UserID    string     `json:"userId"`
}

type ProfileResponse struct {
	Message string      `json:"message"`
	Data    ProfileData `json:"data"`
}

type ProfileExistsData struct {
	Exists bool   `json:"exists"`
	UserID string `json:"userId"`
}

type ProfileExistsResponse struct {
	Message string            `json:"message"`
	Data    ProfileExistsData `json:"data"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}
