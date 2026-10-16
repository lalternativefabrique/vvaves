package transcribe

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ObjectFetcher is the slice of object storage an ObjectSource needs.
type ObjectFetcher interface {
	Download(ctx context.Context, key string) (io.ReadCloser, error)
}

// ObjectSource localizes media held in object storage.
//
// It is what lets the decoder run with no network beyond the bucket: the
// requester has already fetched the source with its own guarded client and
// deposited the bytes, so ffmpeg is handed a local path instead of a URL it
// would have to resolve itself. URLSource leaves that resolution — and the
// DNS-rebinding window with it — to ffmpeg; this does not.
type ObjectSource struct {
	Storage ObjectFetcher
	Key     string
	// Ext names the temp file. Some STT backends pick their demuxer from the
	// filename and fail on a bare one, so the caller passes the extension its
	// sniffer already determined. Empty is allowed: ffmpeg reads the content.
	Ext string
	// MaxBytes bounds what is written to local disk. 0 means no bound.
	MaxBytes int64
}

// Localize downloads the object to a temp file and returns its path.
func (s ObjectSource) Localize(ctx context.Context) (string, func(), error) {
	if s.Storage == nil {
		return "", nil, fmt.Errorf("object source: no storage configured")
	}
	if s.Key == "" {
		return "", nil, fmt.Errorf("object source: no key")
	}

	rc, err := s.Storage.Download(ctx, s.Key)
	if err != nil {
		return "", nil, fmt.Errorf("download %s: %w", s.Key, err)
	}
	defer rc.Close()

	tmpDir, err := os.MkdirTemp("", "transcode-object-*")
	if err != nil {
		return "", nil, fmt.Errorf("temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	path := filepath.Join(tmpDir, "source"+s.Ext)
	f, err := os.Create(path)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}

	var src io.Reader = rc
	if s.MaxBytes > 0 {
		// One byte past the ceiling, so an oversized object is detected rather
		// than silently truncated and transcribed as a valid partial file.
		src = io.LimitReader(rc, s.MaxBytes+1)
	}
	written, err := io.Copy(f, src)
	f.Close()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write temp file: %w", err)
	}
	if s.MaxBytes > 0 && written > s.MaxBytes {
		cleanup()
		return "", nil, fmt.Errorf("object %s exceeds %d bytes", s.Key, s.MaxBytes)
	}

	return path, cleanup, nil
}
