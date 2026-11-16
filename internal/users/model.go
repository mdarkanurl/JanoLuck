package users

import "github.com/mdarkanurl/JanoLuck/internal/httpx"

var FixedQuestions = []string{
	"What is your name?",
	"Do you want predictions that are short and simple, or long and detailed?",
	"Do you want to hear the best-case scenario, the worst-case scenario, or both?",
	"When you get advice, do you prefer clear steps to follow or just ideas to think about?",
	"Should I be serious or a little playful when I talk to you?",
}

type Question struct {
	Index    int    `json:"index"    validate:"min=0"`
	Question string `json:"question" validate:"required,min=1,max=1000"`
	Answer   string `json:"answer"   validate:"required,min=1,max=2000"`
}

type CreateProfileRequest struct {
	QOne   string `json:"qOne"  validate:"required,min=1,max=2000"`
	QTwo   string `json:"qTwo"  validate:"required,min=1,max=2000"`
	QThree string `json:"qThree"  validate:"required,min=1,max=2000"`
	QFour  string `json:"qFour"  validate:"required,min=1,max=2000"`
	QFive  string `json:"qFive"  validate:"required,min=1,max=2000"`
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

func BuildFixedQuestions(qOne, qTwo, qThree, qFour, qFive string) []Question {
	answers := []string{qOne, qTwo, qThree, qFour, qFive}
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

type (
	ValidationError = httpx.ValidationError
	ErrorResponse   = httpx.ErrorResponse
)
