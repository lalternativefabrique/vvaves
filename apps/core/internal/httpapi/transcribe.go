package httpapi

import (
	"context"
	"io"
	"log"
	"net/http"
)

// maxAudio bounds a recording: the provider's own limit for one request.
const maxAudio = 25 << 20

// Transcriber turns a recording into text. stt.Whisper is the one main wires.
type Transcriber interface {
	Transcribe(ctx context.Context, filename string, audio io.Reader, language string) (string, error)
}

// transcribeResponse is the text heard in a recording.
type transcribeResponse struct {
	Text string `json:"text"`
}

// handleTranscribe godoc
// @Summary  Turn a recording into text
// @Description  Takes the service credentials /speak/prime takes, under the vvaves:transcribe scope; a signed listener URL is refused.
// @Tags     transcribe
// @Accept   multipart/form-data
// @Produce  json
// @Param    audio     formData  file    true   "Recording (webm, ogg, m4a, wav, mp3), at most 25 MB"
// @Param    language  formData  string  false  "ISO language code; empty lets the model detect it"
// @Success  200  {object}  transcribeResponse
// @Failure  400  {object}  errorResponse
// @Failure  403  {object}  errorResponse
// @Failure  502  {object}  errorResponse
// @Failure  503  {object}  errorResponse
// @Router   /transcribe [post]
// @ID       transcribe
func handleTranscribe(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Transcriber == nil {
			writeError(w, http.StatusServiceUnavailable, "transcription is not configured")
			return
		}
		if err := d.guardService(r, ScopeTranscribe); err != nil {
			writeAuthError(w, err)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAudio+1<<20)
		file, header, err := r.FormFile("audio")
		if err != nil {
			writeError(w, http.StatusBadRequest, "audio is required, at most 25 MB")
			return
		}
		defer file.Close()
		if header.Size == 0 {
			writeError(w, http.StatusBadRequest, "audio is empty")
			return
		}
		text, err := d.Transcriber.Transcribe(r.Context(), header.Filename, file, r.FormValue("language"))
		if err != nil {
			log.Printf("vvaves: transcribe: %v", err)
			writeError(w, http.StatusBadGateway, "the transcription did not complete")
			return
		}
		writeJSON(w, http.StatusOK, transcribeResponse{Text: text})
	}
}
