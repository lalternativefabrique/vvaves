package config

import "testing"

func TestKeysAreReadByIssuer(t *testing.T) {
	t.Setenv("SPEAK_KEYS", "lalter:aaa, synthiz:bbb")

	keys := Load().Keys
	if got := keys["lalter"]; len(got) != 1 || got[0] != "aaa" {
		t.Errorf("keys[lalter] = %q, want aaa", got)
	}
	if got := keys["synthiz"]; len(got) != 1 || got[0] != "bbb" {
		t.Errorf("keys[synthiz] = %q, want bbb", got)
	}
	if len(keys) != 2 {
		t.Errorf("got %d keys, want 2", len(keys))
	}
}

// An entry with no issuer is dropped rather than read as a bare secret: it
// would authenticate a caller nobody can name, and the operator who wrote it
// believes the whole line took effect.
func TestMalformedKeysAreDropped(t *testing.T) {
	t.Setenv("SPEAK_KEYS", "no-issuer,lalter:aaa,:empty,synthiz:")

	keys := Load().Keys
	if len(keys) != 1 || len(keys["lalter"]) != 1 || keys["lalter"][0] != "aaa" {
		t.Errorf("keys = %v, want only the well-formed pair", keys)
	}
}

func TestNoKeysConfiguredIsEmpty(t *testing.T) {
	t.Setenv("SPEAK_KEYS", "")

	if keys := Load().Keys; len(keys) != 0 {
		t.Errorf("keys = %v, want empty", keys)
	}
}

func TestAnIssuerMayHoldSeveralSecrets(t *testing.T) {
	t.Setenv("SPEAK_KEYS", "lalter:new,lalter:old")

	keys := Load().Keys
	if got := keys["lalter"]; len(got) != 2 || got[0] != "new" || got[1] != "old" {
		t.Errorf("keys[lalter] = %q, want both, in order", got)
	}
}
