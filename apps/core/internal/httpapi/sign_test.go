package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lalternative/packages/vvaves/sdk-go/signed"
	"github.com/lalternativefabrique/vvaves/core/internal/httpapi"
)

const signingSecret = "vvaves-own-secret"

func signingDeps(t *testing.T) httpapi.Deps {
	t.Helper()
	d := guardedDeps(t)
	d.CustomerKeys = &stubCustomerKeys{}
	d.SigningSecret = signingSecret
	d.Verifier = signed.NewLookupVerifier(httpapi.WithSigningIssuer(func(issuer string) []string {
		if issuer == testIssuer {
			return []string{testKey}
		}
		return nil
	}, signingSecret))
	return d
}

func TestSignHandsACustomerKeyAURLThatPlays(t *testing.T) {
	d := signingDeps(t)
	d.PublicURL = "https://voice.example/"
	rec := postBearer(t, d, "/speak/sign", customerKey, speakBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var got struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.URL, "https://voice.example/speak?") {
		t.Fatalf("url = %q", got.URL)
	}
	if time.Until(got.ExpiresAt) < 29*time.Minute {
		t.Fatalf("expires_at = %v", got.ExpiresAt)
	}
	u, _ := url.Parse(got.URL)
	if u.Query().Get(signed.QueryIssuer) != httpapi.SigningIssuer {
		t.Fatalf("iss = %q", u.Query().Get(signed.QueryIssuer))
	}

	play := httptest.NewRecorder()
	httpapi.New(d).ServeHTTP(play, httptest.NewRequest(http.MethodPost, "/speak?"+u.RawQuery, strings.NewReader(speakBody)))
	if play.Code != http.StatusOK {
		t.Fatalf("playing the signed url: status = %d (body %q)", play.Code, play.Body.String())
	}
}

func TestSignTakesThePublicBaseFromTheRequest(t *testing.T) {
	d := signingDeps(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/speak/sign", strings.NewReader(speakBody))
	req.Host = "api.voice.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Authorization", "Bearer a-service-token")
	httpapi.New(d).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"url":"https://api.voice.example/speak?`) {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
}

func TestSignRequiresACredential(t *testing.T) {
	rec := post(t, httpapi.New(signingDeps(t)), "/speak/sign", speakBody)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want a refusal", rec.Code)
	}
}

// A signature that plays one reading must not mint more of them.
func TestSignRefusesASignatureAsCredential(t *testing.T) {
	q, _ := signed.Sign(testIssuer, testKey, signed.Params{
		Scope: "chat", ID: "m1", TextHash: signed.HashText(longText), Expires: time.Now().Add(time.Minute),
	})
	rec := post(t, httpapi.New(signingDeps(t)), "/speak/sign?"+q.Encode(), speakBody)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want a refusal", rec.Code)
	}
}

func TestSignWithoutASecretIsUnavailable(t *testing.T) {
	d := signingDeps(t)
	d.SigningSecret = ""
	if rec := postBearer(t, d, "/speak/sign", customerKey, speakBody); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSignRefusesAnIDTheSignatureCannotSeparate(t *testing.T) {
	rec := postBearer(t, signingDeps(t), "/speak/sign", customerKey, `{"text":"hi","scope":"s","id":"a\nscope=b"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %q)", rec.Code, rec.Body.String())
	}
}
