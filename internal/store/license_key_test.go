package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tabloy/keygate/internal/license"
)

func TestNewLicenseKeyFormatPerProduct(t *testing.T) {
	s := &Store{KeyPolicy: license.NewKeyPolicy([]string{"prod-alc"})}
	never := func(context.Context, string) (bool, error) { return false, nil }

	k, err := s.newLicenseKey(context.Background(), "prod-alc", never)
	if err != nil || !license.ValidALCKey(k) {
		t.Fatalf("ALC product: key %q err %v", k, err)
	}
	k, err = s.newLicenseKey(context.Background(), "prod-keygate", never)
	if err != nil || !strings.HasPrefix(k, "KG-") {
		t.Fatalf("other product: key %q err %v", k, err)
	}
}

func TestNewLicenseKeyRetriesOnCollision(t *testing.T) {
	s := &Store{KeyPolicy: license.NewKeyPolicy([]string{"prod-alc"})}
	calls := 0
	collideTwice := func(context.Context, string) (bool, error) { calls++; return calls <= 2, nil }
	k, err := s.newLicenseKey(context.Background(), "prod-alc", collideTwice)
	if err != nil || !license.ValidALCKey(k) {
		t.Fatalf("key %q err %v", k, err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 existence checks (2 collisions + 1 free), got %d", calls)
	}
}

func TestNewLicenseKeyBoundedRetries(t *testing.T) {
	s := &Store{KeyPolicy: license.NewKeyPolicy([]string{"prod-alc"})}
	calls := 0
	always := func(context.Context, string) (bool, error) { calls++; return true, nil }
	if _, err := s.newLicenseKey(context.Background(), "prod-alc", always); !errors.Is(err, ErrKeyExhausted) {
		t.Fatalf("want ErrKeyExhausted, got %v", err)
	}
	if calls != maxKeyAttempts {
		t.Fatalf("expected exactly %d attempts, got %d", maxKeyAttempts, calls)
	}
}

func TestNewLicenseKeyPropagatesLookupError(t *testing.T) {
	s := &Store{}
	boom := errors.New("db down")
	if _, err := s.newLicenseKey(context.Background(), "p", func(context.Context, string) (bool, error) { return false, boom }); !errors.Is(err, boom) {
		t.Fatalf("want lookup error, got %v", err)
	}
}
