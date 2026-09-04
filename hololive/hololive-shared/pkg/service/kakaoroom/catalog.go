package kakaoroom

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

type irisLister interface {
	GetRooms(ctx context.Context) ([]Facts, error)
}

type roomStore interface {
	upsert(context.Context, Facts) error
	get(context.Context, string) (Facts, bool, error)
}

type Catalog struct {
	store  roomStore
	lister irisLister
	logger *slog.Logger

	mu    sync.RWMutex
	cache map[string]Facts
}

func New(pool *pgxpool.Pool, lister irisLister, logger *slog.Logger) *Catalog {
	if logger == nil {
		logger = slog.Default()
	}

	return &Catalog{
		store:  &store{pool: pool},
		lister: lister,
		logger: logger,
		cache:  make(map[string]Facts),
	}
}

func (c *Catalog) Observe(ctx context.Context, roomID, roomType, roomLinkID string) {
	if c == nil {
		return
	}

	facts := normalizeFacts(roomID, roomType, roomLinkID)
	if facts.RoomID == "" || (facts.RoomType == "" && facts.RoomLinkID == "") {
		return
	}

	c.remember(facts)
	c.warn(ctx, "observe kakao room failed", c.store.upsert(ctx, facts))
}

func (c *Catalog) OpenChat(ctx context.Context, roomID string) bool {
	if c == nil {
		return false
	}

	facts, ok := c.lookup(ctx, roomID)

	return ok && facts.OpenChat()
}

// RegularChat은 room facts가 존재하고 오픈채팅이 아닐 때만 true를 반환합니다.
func (c *Catalog) RegularChat(ctx context.Context, roomID string) bool {
	if c == nil {
		return false
	}

	facts, ok := c.lookup(ctx, roomID)

	return ok && !facts.OpenChat()
}

func (c *Catalog) lookup(ctx context.Context, roomID string) (Facts, bool) {
	roomID = normalizeFacts(roomID, "", "").RoomID
	if roomID == "" {
		return Facts{}, false
	}

	if facts, ok := c.cached(roomID); ok {
		return facts, true
	}

	facts, ok, err := c.loadStored(ctx, roomID)
	if err != nil || ok {
		return facts, ok
	}

	return c.loadAfterRefresh(ctx, roomID)
}

func (c *Catalog) loadStored(ctx context.Context, roomID string) (Facts, bool, error) {
	facts, ok, err := c.store.get(ctx, roomID)
	if err != nil {
		c.warn(ctx, "load kakao room failed", err)

		return Facts{}, false, fmt.Errorf("get: %w", err)
	}

	if !ok {
		return Facts{}, false, nil
	}

	c.remember(facts)

	return facts, true, nil
}

func (c *Catalog) loadAfterRefresh(ctx context.Context, roomID string) (Facts, bool) {
	if !c.refresh(ctx) {
		return Facts{}, false
	}

	return c.cached(roomID)
}

func (c *Catalog) refresh(ctx context.Context) bool {
	if c.lister == nil {
		return false
	}

	rooms, err := c.lister.GetRooms(ctx)
	if err != nil {
		c.warn(ctx, "list kakao rooms failed", err)

		return false
	}

	c.storeListed(ctx, rooms)

	return true
}

func (c *Catalog) storeListed(ctx context.Context, rooms []Facts) {
	for _, facts := range rooms {
		if !usableListedFacts(facts) {
			continue
		}

		c.remember(facts)
		c.warn(ctx, "store kakao room failed", c.store.upsert(ctx, facts))
	}
}

func usableListedFacts(facts Facts) bool {
	return facts.RoomID != "" && (facts.RoomType != "" || facts.RoomLinkID != "")
}

func (c *Catalog) warn(ctx context.Context, msg string, err error) {
	if c.logger == nil || err == nil {
		return
	}

	c.logger.LogAttrs(ctx, slog.LevelWarn, msg, slog.String("error", err.Error()))
}

func (c *Catalog) remember(facts Facts) {
	c.mu.Lock()

	c.cache[facts.RoomID] = facts
	c.mu.Unlock()
}

func (c *Catalog) cached(roomID string) (Facts, bool) {
	c.mu.RLock()

	facts, ok := c.cache[roomID]
	c.mu.RUnlock()

	return facts, ok
}
