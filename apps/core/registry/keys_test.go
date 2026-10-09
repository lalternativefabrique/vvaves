package registry

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/lalternativefabrique/vvaves/core/registry/application"
	"github.com/lalternativefabrique/vvaves/core/registry/domain"
	"github.com/lalternativefabrique/vvaves/core/registry/infrastructure"
)

type fakeLister struct {
	apps  []*domain.App
	calls int
}

func (f *fakeLister) List(context.Context) ([]*domain.App, error) {
	f.calls++
	return f.apps, nil
}

func testCipher(t *testing.T) *infrastructure.Cipher {
	t.Helper()
	c, err := infrastructure.NewCipherFromBase64(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRegistryWinsOverTheEnvironmentAndBothAreServed(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	cipher := testCipher(t)
	key, sealed, _ := application.Mint(cipher)
	partage, _ := domain.Register("partage", sealed, key, now)
	lister := &fakeLister{apps: []*domain.App{partage}}
	k := NewKeySource(lister, cipher,
		map[string][]string{"partage": {"old-env-secret"}, "lalter": {"lalter-secret"}})
	k.now = func() time.Time { return now }

	if got := k.Keys("partage"); len(got) != 1 || got[0] != key {
		t.Fatalf("partage keys = %v, want the registry's alone", got)
	}
	if got := k.Keys("lalter"); len(got) != 1 || got[0] != "lalter-secret" {
		t.Fatalf("lalter keys = %v, want the environment's", got)
	}
}

// A rotated app answers with both keys while its grace lasts, and a revoked
// one with none.
func TestRotationAndRevocationReachTheGuard(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	cipher := testCipher(t)
	k1, s1, _ := application.Mint(cipher)
	rotated, _ := domain.Register("lalter", s1, k1, now.Add(-time.Hour))
	k2, s2, _ := application.Mint(cipher)
	rotated.Rotate(s2, k2, now.Add(-time.Minute))
	k3, s3, _ := application.Mint(cipher)
	revoked, _ := domain.Register("synthiz", s3, k3, now)
	revoked.Revoke(now)

	k := NewKeySource(&fakeLister{apps: []*domain.App{rotated, revoked}}, cipher, nil)
	k.now = func() time.Time { return now }
	if got := k.Keys("lalter"); len(got) != 2 || got[0] != k2 || got[1] != k1 {
		t.Fatalf("lalter keys = %v, want current then previous", got)
	}
	if got := k.Keys("synthiz"); len(got) != 0 {
		t.Fatalf("synthiz keys = %v, want none once revoked", got)
	}
}

func TestKeysAreCachedUntilInvalidatedOrStale(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	lister := &fakeLister{}
	k := NewKeySource(lister, testCipher(t), nil)
	k.now = func() time.Time { return now }

	k.Keys("x")
	k.Keys("x")
	if lister.calls != 1 {
		t.Fatalf("calls = %d, want one fetch within the ttl", lister.calls)
	}
	k.Invalidate()
	k.Keys("x")
	if lister.calls != 2 {
		t.Fatalf("calls = %d, want a refetch after Invalidate", lister.calls)
	}
	now = now.Add(time.Minute)
	k.Keys("x")
	if lister.calls != 3 {
		t.Fatalf("calls = %d, want a refetch once stale", lister.calls)
	}
}
