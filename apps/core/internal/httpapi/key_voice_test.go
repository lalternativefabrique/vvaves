package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/audioreader"

	"github.com/lalternativefabrique/vvaves/core/internal/audio"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

func keyVoiceDeps(t *testing.T) httpapi.Deps {
	t.Helper()
	d := signingDeps(t)
	d.CustomerKeys = &stubCustomerKeys{clientID: "ak_messag"}
	chosen := httpapi.Voice{Reader: audioreader.NewReader(audio.NewProvider(&stubVoice{pieces: [][]byte{[]byte("messag-voice")}}), nil, testOpeningChars, nil)}
	d.KeyVoice = func(_ context.Context, keyID string) (httpapi.Voice, string, bool) {
		if keyID == "ak_messag" {
			return chosen, "v-messag", true
		}
		return httpapi.Voice{}, "", false
	}
	return d
}

func TestSpeakReadsInTheVoiceChosenForTheKey(t *testing.T) {
	rec := postBearer(t, keyVoiceDeps(t), "/speak", customerKey, speakBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Tts-Voice"); got != "v-messag" {
		t.Fatalf("X-Tts-Voice = %q, want v-messag", got)
	}
}

func TestASignedURLKeepsTheVoiceOfTheKeyThatAskedForIt(t *testing.T) {
	d := keyVoiceDeps(t)
	rec := postBearer(t, d, "/speak/sign", customerKey, speakBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	var got struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got.URL)
	if u.Query().Get(httpapi.QueryKey) != "ak_messag" {
		t.Fatalf("url = %q, want the key in it", got.URL)
	}

	play := httptest.NewRecorder()
	httpapi.New(d).ServeHTTP(play, httptest.NewRequest(http.MethodPost, "/speak?"+u.RawQuery, strings.NewReader(speakBody)))
	if play.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", play.Code, play.Body.String())
	}
	if got := play.Header().Get("X-Tts-Voice"); got != "v-messag" {
		t.Fatalf("X-Tts-Voice = %q, want v-messag", got)
	}
}

func TestAKeyWithoutAVoiceKeepsTheDeploymentsVoices(t *testing.T) {
	d := keyVoiceDeps(t)
	d.CustomerKeys = &stubCustomerKeys{clientID: "ak_other"}
	rec := postBearer(t, d, "/speak", customerKey, speakBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Tts-Voice"); got != "default" {
		t.Fatalf("X-Tts-Voice = %q, want default", got)
	}
}
