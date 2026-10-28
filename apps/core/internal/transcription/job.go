// Package transcription runs long transcriptions in the background: a caller
// submits an audio URL, vvaves fetches it, transcribes it in chunks and
// announces the result on the suite's bus.
package transcription

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

const maxReferenceLen = 200

var (
	ErrInvalid  = errors.New("invalid transcription request")
	ErrNotFound = errors.New("transcription not found")
	ErrBusy     = errors.New("too many transcriptions waiting")
)

type Segment struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

type Result struct {
	Language        string    `json:"language"`
	DurationSeconds float64   `json:"duration_seconds"`
	Text            string    `json:"text"`
	Segments        []Segment `json:"segments"`
}

type Job struct {
	ID         string
	App        string
	EndUser    string
	Reference  string
	URL        string
	Language   string
	Status     Status
	Error      string
	Result     *Result
	CreatedAt  time.Time
	FinishedAt *time.Time
}

type Request struct {
	App       string
	EndUser   string
	URL       string
	Language  string
	Reference string
}

// Validate admits only https audio URLs; plain http is left to loopback
// hosts, which only a test or a laptop serves from.
func (r Request) Validate() error {
	if r.Reference == "" {
		return fmt.Errorf("%w: reference is required", ErrInvalid)
	}
	if len(r.Reference) > maxReferenceLen {
		return fmt.Errorf("%w: reference exceeds %d characters", ErrInvalid, maxReferenceLen)
	}
	u, err := url.Parse(r.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: url must be an absolute https URL", ErrInvalid)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
	}
	return fmt.Errorf("%w: url must be an absolute https URL", ErrInvalid)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
