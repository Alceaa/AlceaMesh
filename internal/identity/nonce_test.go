package identity

import (
	"testing"
	"time"
)

func TestNonceCacheDetectsReplay(t *testing.T) {
	cache := NewNonceCache(time.Minute, 10)
	now := time.Unix(1700000000, 0)

	if cache.Seen("dev-1", "aaaa", now) {
		t.Fatal("the first use must not look like a replay")
	}
	if !cache.Seen("dev-1", "aaaa", now.Add(time.Second)) {
		t.Fatal("the second use must be detected as a replay")
	}
	if cache.Seen("dev-2", "aaaa", now) {
		t.Error("the same nonce from another device must be allowed")
	}
	if cache.Seen("dev-1", "bbbb", now) {
		t.Error("a fresh nonce must be allowed")
	}
}

func TestNonceCacheExpires(t *testing.T) {
	cache := NewNonceCache(time.Minute, 10)
	now := time.Unix(1700000000, 0)

	cache.Seen("dev-1", "aaaa", now)
	if cache.Size("dev-1") != 1 {
		t.Fatalf("size = %d, want 1", cache.Size("dev-1"))
	}

	if cache.Seen("dev-1", "aaaa", now.Add(2*time.Minute)) {
		t.Error("an expired nonce must be forgotten")
	}
	if cache.Size("dev-1") != 1 {
		t.Errorf("size after cleanup = %d, want 1", cache.Size("dev-1"))
	}
}

func TestNonceCacheRespectsTheLimit(t *testing.T) {
	cache := NewNonceCache(time.Hour, 3)
	now := time.Unix(1700000000, 0)

	for index, nonce := range []string{"n1", "n2", "n3"} {
		if cache.Seen("dev-1", nonce, now.Add(time.Duration(index)*time.Second)) {
			t.Fatalf("nonce %s must be fresh", nonce)
		}
	}
	if cache.Size("dev-1") != 3 {
		t.Fatalf("size = %d, want 3", cache.Size("dev-1"))
	}

	cache.Seen("dev-1", "n4", now.Add(10*time.Second))
	if cache.Size("dev-1") != 3 {
		t.Errorf("size = %d, want 3 after dropping the oldest", cache.Size("dev-1"))
	}
	if cache.Seen("dev-1", "n1", now.Add(11*time.Second)) {
		t.Error("the oldest nonce must have been dropped")
	}
}

func TestNonceCacheDefaults(t *testing.T) {
	cache := NewNonceCache(0, 0)
	if cache.ttl != DefaultNonceTTL || cache.limit != DefaultNonceLimit {
		t.Errorf("ttl = %s, limit = %d", cache.ttl, cache.limit)
	}
	if cache.Devices() != 0 {
		t.Errorf("devices = %d, want 0", cache.Devices())
	}
}
