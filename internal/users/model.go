package users

var FixedQuestions = []string{
	"Where are you from?",
	"What do you do for living?",
}

type Question struct {
	Question string `json:"question" validate:"required,min=1,max=1000"`
	Answer   string `json:"answer"   validate:"required,min=1,max=2000"`
}

type CreateProfileRequest struct {
	Name string `json:"name"  validate:"required,min=1,max=100"`
	QOne string `json:"qOne"  validate:"required,min=1,max=2000"`
	QTwo string `json:"qTwo"  validate:"required,min=1,max=2000"`
}

type UpdateProfileRequest struct {
	Name string `json:"name"  validate:"required,min=1,max=100"`
	QOne string `json:"qOne"  validate:"required,min=1,max=2000"`
	QTwo string `json:"qTwo"  validate:"required,min=1,max=2000"`
}

func BuildFixedQuestions(qOne, qTwo string) []Question {
	answers := []string{qOne, qTwo}
	questions := make([]Question, len(FixedQuestions))
	for i, q := range FixedQuestions {
		questions[i] = Question{Question: q, Answer: answers[i]}
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

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}
