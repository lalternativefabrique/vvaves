package transcription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

var errFetch = errors.New("fetch audio")

// urlFetcher downloads the one URL a job names. Its errors never quote the
// URL: a presigned one carries a credential.
type urlFetcher struct {
	client *http.Client
	url    string
}

func (f urlFetcher) Download(ctx context.Context, _ string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request", errFetch)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFetch, redact(err))
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("%w: status %d", errFetch, res.StatusCode)
	}
	return res.Body, nil
}

func redact(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err
	}
	return err
}

var audioExtensions = map[string]bool{
	".mp3": true, ".m4a": true, ".mp4": true, ".wav": true, ".ogg": true,
	".oga": true, ".opus": true, ".webm": true, ".flac": true, ".aac": true,
}

// extensionOf names the temp file after the URL's own extension, because some
// STT backends pick their demuxer from the file name.
func extensionOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if audioExtensions[ext] {
		return ext
	}
	return ""
}
