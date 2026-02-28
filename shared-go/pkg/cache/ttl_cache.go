package cache

import (
	"container/list"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const ttlCachePurgeLimit = 8

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time
}

// asEntry는 list.Element의 값을 *entry[K,V]로 안전하게 변환합니다.
// 내부 invariant 위반 시 nil 반환 + 로그 기록.
func asEntry[K comparable, V any](el *list.Element) *entry[K, V] {
	if el == nil {
		return nil
	}
	ent, ok := el.Value.(*entry[K, V])
	if !ok {
		slog.Error("ttl_cache: invalid entry type in list element",
			"actual_type", fmt.Sprintf("%T", el.Value))
		return nil
	}
	return ent
}

// TTLCache는 만료 시간과 최대 크기를 가진 LRU 캐시입니다.
type TTLCache[K comparable, V any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	order   *list.List
	items   map[K]*list.Element
	enabled bool
}

// NewTTLCache는 만료 시간과 최대 크기를 갖는 TTLCache를 생성합니다.
// maxSize 또는 ttl이 0 이하이면 비활성 캐시를 반환합니다.
func NewTTLCache[K comparable, V any](maxSize int, ttl time.Duration) *TTLCache[K, V] {
	if maxSize <= 0 || ttl <= 0 {
		return &TTLCache[K, V]{enabled: false}
	}
	return &TTLCache[K, V]{
		ttl:     ttl,
		maxSize: maxSize,
		order:   list.New(),
		items:   make(map[K]*list.Element, maxSize),
		enabled: true,
	}
}

// Get은 캐시에서 값을 조회합니다.
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	var zero V
	if c == nil || !c.enabled {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[key]
	if !ok {
		return zero, false
	}

	ent := asEntry[K, V](element)
	if ent == nil {
		c.removeElement(element)
		return zero, false
	}
	if time.Now().After(ent.expiresAt) {
		c.removeElement(element)
		return zero, false
	}

	c.order.MoveToFront(element)
	return ent.value, true
}

// Set은 캐시에 값을 저장합니다.
func (c *TTLCache[K, V]) Set(key K, value V) {
	if c == nil || !c.enabled {
		return
	}
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.items[key]; ok {
		ent := asEntry[K, V](element)
		if ent == nil {
			// invariant 위반: 리스트와 맵에서 제거 후 새 엔트리로 대체
			c.order.Remove(element)
			delete(c.items, key)
		} else {
			ent.value = value
			ent.expiresAt = now.Add(c.ttl)
			c.order.MoveToFront(element)
			return
		}
	}

	ent := &entry[K, V]{
		key:       key,
		value:     value,
		expiresAt: now.Add(c.ttl),
	}
	element := c.order.PushFront(ent)
	c.items[key] = element
	c.purgeExpired(now, ttlCachePurgeLimit)
	c.evictIfNeeded()
}

// Modify는 key의 값을 원자적으로 갱신하고 갱신된 값을 반환합니다.
// update 함수는 캐시 내부 락을 잡은 상태로 호출되므로, update 내부에서 긴 연산을 수행하지 않아야 합니다.
func (c *TTLCache[K, V]) Modify(key K, update func(current V, exists bool) V) (V, bool) {
	var zero V
	if c == nil || !c.enabled || update == nil {
		return zero, false
	}
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if element, ok := c.items[key]; ok {
		ent := asEntry[K, V](element)
		if ent == nil {
			// invariant 위반: 리스트와 맵에서 제거 후 새 엔트리로 대체
			c.order.Remove(element)
			delete(c.items, key)
		} else if now.After(ent.expiresAt) {
			c.removeElement(element)
		} else {
			ent.value = update(ent.value, true)
			ent.expiresAt = now.Add(c.ttl)
			c.order.MoveToFront(element)
			return ent.value, true
		}
	}

	value := update(zero, false)
	ent := &entry[K, V]{
		key:       key,
		value:     value,
		expiresAt: now.Add(c.ttl),
	}
	element := c.order.PushFront(ent)
	c.items[key] = element
	c.purgeExpired(now, ttlCachePurgeLimit)
	c.evictIfNeeded()
	return value, true
}

// Delete는 캐시에서 값을 삭제합니다.
func (c *TTLCache[K, V]) Delete(key K) {
	if c == nil || !c.enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	element, ok := c.items[key]
	if !ok {
		return
	}
	c.removeElement(element)
}

// Len은 캐시의 현재 항목 수를 반환합니다.
func (c *TTLCache[K, V]) Len() int {
	if c == nil || !c.enabled {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *TTLCache[K, V]) purgeExpired(now time.Time, limit int) {
	for range limit {
		element := c.order.Back()
		if element == nil {
			return
		}
		ent := asEntry[K, V](element)
		if ent == nil {
			// invariant 위반: 리스트에서만 제거 (키를 알 수 없으므로 맵 정리 불가)
			c.order.Remove(element)
			continue
		}
		if now.Before(ent.expiresAt) {
			return
		}
		c.removeElement(element)
	}
}

func (c *TTLCache[K, V]) evictIfNeeded() {
	for len(c.items) > c.maxSize {
		element := c.order.Back()
		if element == nil {
			return
		}
		c.removeElement(element)
	}
}

func (c *TTLCache[K, V]) removeElement(element *list.Element) {
	c.order.Remove(element)
	ent := asEntry[K, V](element)
	if ent == nil {
		// invariant 위반: 키를 알 수 없으므로 맵 정리 불가
		return
	}
	delete(c.items, ent.key)
}
