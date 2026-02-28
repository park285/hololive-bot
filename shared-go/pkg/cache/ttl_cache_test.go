package cache

import (
	"testing"
	"time"
)

func TestNewTTLCache(t *testing.T) {
	t.Run("valid parameters", func(t *testing.T) {
		cache := NewTTLCache[string, int](10, 5*time.Second)
		if cache == nil {
			t.Fatal("expected non-nil cache")
		}
		if !cache.enabled {
			t.Error("expected cache to be enabled")
		}
		if cache.maxSize != 10 {
			t.Errorf("expected maxSize 10, got %d", cache.maxSize)
		}
		if cache.ttl != 5*time.Second {
			t.Errorf("expected ttl 5s, got %v", cache.ttl)
		}
	})

	t.Run("invalid maxSize", func(t *testing.T) {
		cache := NewTTLCache[string, int](0, 5*time.Second)
		if cache.enabled {
			t.Error("expected cache to be disabled")
		}
	})

	t.Run("invalid ttl", func(t *testing.T) {
		cache := NewTTLCache[string, int](10, 0)
		if cache.enabled {
			t.Error("expected cache to be disabled")
		}
	})
}

func TestTTLCache_GetSet(t *testing.T) {
	cache := NewTTLCache[string, int](10, 1*time.Second)

	t.Run("get from empty cache", func(t *testing.T) {
		val, ok := cache.Get("key1")
		if ok {
			t.Error("expected false for missing key")
		}
		if val != 0 {
			t.Errorf("expected zero value, got %d", val)
		}
	})

	t.Run("set and get", func(t *testing.T) {
		cache.Set("key1", 100)
		val, ok := cache.Get("key1")
		if !ok {
			t.Error("expected true for existing key")
		}
		if val != 100 {
			t.Errorf("expected 100, got %d", val)
		}
	})

	t.Run("update existing key", func(t *testing.T) {
		cache.Set("key1", 200)
		val, ok := cache.Get("key1")
		if !ok {
			t.Error("expected true for existing key")
		}
		if val != 200 {
			t.Errorf("expected 200, got %d", val)
		}
	})
}

func TestTTLCache_Expiration(t *testing.T) {
	cache := NewTTLCache[string, int](10, 100*time.Millisecond)

	cache.Set("key1", 100)
	time.Sleep(150 * time.Millisecond)

	val, ok := cache.Get("key1")
	if ok {
		t.Error("expected false for expired key")
	}
	if val != 0 {
		t.Errorf("expected zero value for expired key, got %d", val)
	}
}

func TestTTLCache_Delete(t *testing.T) {
	cache := NewTTLCache[string, int](10, 5*time.Second)

	cache.Set("key1", 100)
	cache.Delete("key1")

	val, ok := cache.Get("key1")
	if ok {
		t.Error("expected false after delete")
	}
	if val != 0 {
		t.Errorf("expected zero value, got %d", val)
	}
}

func TestTTLCache_Len(t *testing.T) {
	cache := NewTTLCache[string, int](10, 5*time.Second)

	if cache.Len() != 0 {
		t.Errorf("expected length 0, got %d", cache.Len())
	}

	cache.Set("key1", 100)
	cache.Set("key2", 200)

	if cache.Len() != 2 {
		t.Errorf("expected length 2, got %d", cache.Len())
	}

	cache.Delete("key1")

	if cache.Len() != 1 {
		t.Errorf("expected length 1, got %d", cache.Len())
	}
}

func TestTTLCache_Modify(t *testing.T) {
	cache := NewTTLCache[string, int](10, 5*time.Second)

	t.Run("modify non-existent key", func(t *testing.T) {
		val, ok := cache.Modify("key1", func(current int, exists bool) int {
			if exists {
				t.Error("expected exists to be false")
			}
			return 100
		})
		if !ok {
			t.Error("expected true")
		}
		if val != 100 {
			t.Errorf("expected 100, got %d", val)
		}
	})

	t.Run("modify existing key", func(t *testing.T) {
		val, ok := cache.Modify("key1", func(current int, exists bool) int {
			if !exists {
				t.Error("expected exists to be true")
			}
			if current != 100 {
				t.Errorf("expected current 100, got %d", current)
			}
			return current + 50
		})
		if !ok {
			t.Error("expected true")
		}
		if val != 150 {
			t.Errorf("expected 150, got %d", val)
		}
	})

	t.Run("modify with nil update function", func(t *testing.T) {
		_, ok := cache.Modify("key1", nil)
		if ok {
			t.Error("expected false for nil update function")
		}
	})
}

func TestTTLCache_Eviction(t *testing.T) {
	cache := NewTTLCache[string, int](3, 5*time.Second)

	cache.Set("key1", 100)
	cache.Set("key2", 200)
	cache.Set("key3", 300)
	cache.Set("key4", 400)

	if cache.Len() != 3 {
		t.Errorf("expected length 3 after eviction, got %d", cache.Len())
	}

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected key1 to be evicted")
	}

	if _, ok := cache.Get("key2"); !ok {
		t.Error("expected key2 to exist")
	}
	if _, ok := cache.Get("key3"); !ok {
		t.Error("expected key3 to exist")
	}
	if _, ok := cache.Get("key4"); !ok {
		t.Error("expected key4 to exist")
	}
}

func TestTTLCache_DisabledCache(t *testing.T) {
	cache := NewTTLCache[string, int](0, 5*time.Second)

	cache.Set("key1", 100)
	val, ok := cache.Get("key1")
	if ok {
		t.Error("expected false for disabled cache")
	}
	if val != 0 {
		t.Errorf("expected zero value, got %d", val)
	}

	if cache.Len() != 0 {
		t.Errorf("expected length 0 for disabled cache, got %d", cache.Len())
	}
}

func TestTTLCache_NilCache(t *testing.T) {
	var cache *TTLCache[string, int]

	cache.Set("key1", 100)
	val, ok := cache.Get("key1")
	if ok {
		t.Error("expected false for nil cache")
	}
	if val != 0 {
		t.Errorf("expected zero value, got %d", val)
	}

	cache.Delete("key1")
	if cache.Len() != 0 {
		t.Errorf("expected length 0 for nil cache, got %d", cache.Len())
	}
}
