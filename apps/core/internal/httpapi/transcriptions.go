package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/lalternativefabrique/vvaves/core/internal/transcription"
)

// Transcriptions runs long transcriptions in the background.
// transcription.Service is the one main wires.
type Transcriptions interface {
	Submit(ctx context.Context, r transcription.Request) (transcription.Job, error)
	Get(ctx context.Context, id string) (transcription.Job, error)
}

// transcriptionRequest asks for a recording to be transcribed in the background.
type transcriptionRequest struct {
	// URL the audio is fetched from, https only; a presigned GET works.
	URL string `json:"url" example:"https://bucket.s3.fr-par.scw.cloud/episode.mp3?X-Amz-Signature=..."`
	// Language is an ISO code; empty lets the model detect it.
	Language string `json:"language,omitempty" example:"fr"`
	// Reference is the caller's own id for the recording, echoed in the events.
	Reference string `json:"reference" example:"episode-42"`
}

// transcriptionAccepted names the transcription the events will carry.
type transcriptionAccepted struct {
	ID string `json:"id" example:"5b0e7c1a-3f7e-4d55-9a57-2f9d1c6e0b41"`
}

// transcriptionResponse is a transcription's state, and its result once completed.
type transcriptionResponse struct {
	ID         string                `json:"id"`
	Reference  string                `json:"reference"`
	Status     string                `json:"status" enums:"pending,completed,failed"`
	Error      string                `json:"error,omitempty"`
	Result     *transcription.Result `json:"result,omitempty"`
	CreatedAt  time.Time             `json:"created_at"`
	FinishedAt *time.Time            `json:"finished_at,omitempty"`
}

// handleCreateTranscription godoc
// @Summary  Transcribe a long recording in the background
// @Description  Takes the credentials /transcribe takes. The result is announced on the suite bus as events.vvaves.transcription.completed (or .failed), and can be read from GET /transcriptions/{id}.
// @Tags     transcribe
// @Accept   json
// @Produce  json
// @Param    body  body  transcriptionRequest  true  "Recording to transcribe"
// @Success  202  {object}  transcriptionAccepted
// @Failure  400  {object}  errorResponse
// @Failure  403  {object}  errorResponse
// @Failure  503  {object}  errorResponse
// @Router   /transcriptions [post]
// @ID       createTranscription
func handleCreateTranscription(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Transcriptions == nil {
			writeError(w, http.StatusServiceUnavailable, "transcription is not configured")
			return
		}
		app, err := d.guardService(r, ScopeTranscribe)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		var body transcriptionRequest
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "body must be a JSON object")
			return
		}
		job, err := d.Transcriptions.Submit(r.Context(), transcription.Request{
			App:       app,
			URL:       body.URL,
			Language:  body.Language,
			Reference: body.Reference,
		})
		switch {
		case errors.Is(err, transcription.ErrInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, transcription.ErrBusy):
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusServiceUnavailable, "too many transcriptions waiting, retry")
		case err != nil:
			log.Printf("vvaves: transcription: %v", err)
			writeError(w, http.StatusInternalServerError, "the transcription was not accepted")
		default:
			writeJSON(w, http.StatusAccepted, transcriptionAccepted{ID: job.ID})
		}
	}
}

// handleGetTranscription godoc
// @Summary  Read a background transcription
// @Description  Only the application that submitted it can read it.
// @Tags     transcribe
// @Produce  json
// @Param    id  path  string  true  "Transcription id"
// @Success  200  {object}  transcriptionResponse
// @Failure  403  {object}  errorResponse
// @Failure  404  {object}  errorResponse
// @Failure  503  {object}  errorResponse
// @Router   /transcriptions/{id} [get]
// @ID       getTranscription
func handleGetTranscription(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Transcriptions == nil {
			writeError(w, http.StatusServiceUnavailable, "transcription is not configured")
			return
		}
		app, err := d.guardService(r, ScopeTranscribe)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		job, err := d.Transcriptions.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, transcription.ErrNotFound) || (err == nil && job.App != app) {
			writeError(w, http.StatusNotFound, "no such transcription")
			return
		}
		if err != nil {
			log.Printf("vvaves: transcription: %v", err)
			writeError(w, http.StatusInternalServerError, "the transcription could not be read")
			return
		}
		writeJSON(w, http.StatusOK, transcriptionResponse{
			ID:         job.ID,
			Reference:  job.Reference,
			Status:     string(job.Status),
			Error:      job.Error,
			Result:     job.Result,
			CreatedAt:  job.CreatedAt,
			FinishedAt: job.FinishedAt,
		})
	}
}
