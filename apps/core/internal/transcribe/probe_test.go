package transcribe

import (
	"errors"
	"testing"
)

// The verdict is what the pipeline acts on, so it is tested on ffprobe's real
// output shapes rather than through the binary: the parsing is the decision.
func TestParseProbe_Verdicts(t *testing.T) {
	cases := map[string]struct {
		raw     string
		want    error
		seconds float64
		codec   string
	}{
		"audio with duration": {
			raw:     `{"streams":[{"codec_name":"mp3"}],"format":{"duration":"3600.024"}}`,
			seconds: 3600.024,
			codec:   "mp3",
		},
		"video container carrying audio": {
			raw:     `{"streams":[{"codec_name":"aac"}],"format":{"duration":"125.5"}}`,
			seconds: 125.5,
			codec:   "aac",
		},
		"no audio track": {
			raw:  `{"streams":[],"format":{"duration":"120.0"}}`,
			want: ErrNoAudioStream,
		},
		"empty codec name": {
			raw:  `{"streams":[{"codec_name":""}],"format":{"duration":"120.0"}}`,
			want: ErrNoAudioStream,
		},
		// What a live stream looks like from here: ffprobe opens it, finds
		// audio, and cannot say when it ends.
		"live stream reports no duration": {
			raw:  `{"streams":[{"codec_name":"aac"}],"format":{}}`,
			want: ErrUnboundedDuration,
		},
		"duration N/A": {
			raw:  `{"streams":[{"codec_name":"aac"}],"format":{"duration":"N/A"}}`,
			want: ErrUnboundedDuration,
		},
		"zero duration": {
			raw:  `{"streams":[{"codec_name":"aac"}],"format":{"duration":"0"}}`,
			want: ErrUnboundedDuration,
		},
		"not json": {
			raw:  `ffprobe: command not found`,
			want: ErrUnreadableMedia,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseProbe(tc.raw)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProbe: %v", err)
			}
			if got.DurationSeconds != tc.seconds {
				t.Errorf("DurationSeconds = %v, want %v", got.DurationSeconds, tc.seconds)
			}
			if got.Codec != tc.codec {
				t.Errorf("Codec = %q, want %q", got.Codec, tc.codec)
			}
		})
	}
}

// A probe that cannot answer must refuse rather than guess. copyExtension
// deliberately falls back to .m4a when it cannot tell; this one exists
// precisely because that leniency has no place before paying for STT.
func TestParseProbe_RefusesRatherThanDefaults(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"streams":[]}`,
		`{"streams":[{"codec_name":"aac"}]}`,
	} {
		if _, err := parseProbe(raw); err == nil {
			t.Errorf("parseProbe(%s) = nil error, want a refusal", raw)
		}
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("moov atom not found\nsecond line", errors.New("exit 1")); got != "moov atom not found" {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine("   ", errors.New("exit 1")); got != "exit 1" {
		t.Errorf("empty stderr should fall back to the error: %q", got)
	}
}
