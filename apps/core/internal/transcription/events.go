package transcription

import (
	"fmt"

	"github.com/lalternative/packages/go/busevents"
)

const product = "vvaves"

var (
	SubjectCompleted = busevents.Subject(product, "transcription.completed")
	SubjectFailed    = busevents.Subject(product, "transcription.failed")
)

type Completed struct {
	busevents.Meta
	TranscriptionID string    `json:"id"`
	App             string    `json:"app"`
	Reference       string    `json:"reference"`
	Language        string    `json:"language"`
	DurationSeconds float64   `json:"duration_seconds"`
	Text            string    `json:"text"`
	Segments        []Segment `json:"segments"`
}

func (e Completed) Validate() error {
	if err := validateMeta(e.Meta); err != nil {
		return err
	}
	return requireID(e.TranscriptionID)
}

type Failed struct {
	busevents.Meta
	TranscriptionID string `json:"id"`
	App             string `json:"app"`
	Reference       string `json:"reference"`
	Error           string `json:"error"`
}

func (e Failed) Validate() error {
	if err := validateMeta(e.Meta); err != nil {
		return err
	}
	return requireID(e.TranscriptionID)
}

func validateMeta(m busevents.Meta) error {
	if m.EventID == "" {
		return fmt.Errorf("%w: no event_id", busevents.ErrInvalid)
	}
	if m.OccurredAt.IsZero() {
		return fmt.Errorf("%w: no occurred_at", busevents.ErrInvalid)
	}
	return nil
}

func requireID(id string) error {
	if id == "" {
		return fmt.Errorf("%w: no id", busevents.ErrInvalid)
	}
	return nil
}
