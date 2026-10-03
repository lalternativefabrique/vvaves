package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/audioreader"

	"github.com/lalternativefabrique/vvaves/core/internal/audio"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

func TestSpeakPicksTheListenersVoice(t *testing.T) {
	fallback := &stubVoice{pieces: [][]byte{[]byte("default")}}
	french := &stubVoice{pieces: [][]byte{[]byte("fr")}}
	english := &stubVoice{pieces: [][]byte{[]byte("en")}}

	d := audioDeps(fallback, nil)
	d.Voices = map[string]httpapi.Voice{
		"fr": {Reader: audioreader.NewReader(audio.NewProvider(french), nil, testOpeningChars, nil)},
		"en": {Reader: audioreader.NewReader(audio.NewProvider(english), nil, testOpeningChars, nil)},
	}
	h := httpapi.New(d)

	cases := []struct {
		name, body, acceptLanguage, want string
	}{
		{"lang field", `{"text":"a","lang":"fr-FR"}`, "", "fr"},
		{"lang field wins over the header", `{"text":"b","lang":"en"}`, "fr-FR,fr;q=0.9", "en"},
		{"accept-language", `{"text":"c"}`, "fr-CA;q=0.9,en;q=0.8", "fr"},
		{"unknown language", `{"text":"d","lang":"ja"}`, "", "default"},
		{"no language", `{"text":"e"}`, "", "default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/speak", strings.NewReader(c.body))
			if c.acceptLanguage != "" {
				req.Header.Set("Accept-Language", c.acceptLanguage)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("got %d: %s", rec.Code, rec.Body)
			}
			if got := rec.Body.String(); got != c.want {
				t.Errorf("read by %q, want %q", got, c.want)
			}
		})
	}
}
