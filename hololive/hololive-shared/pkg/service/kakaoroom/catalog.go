package kakaoroom

import (
	"context"
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
	if err := c.store.upsert(ctx, facts); err != nil && c.logger != nil {
		c.logger.LogAttrs(ctx, slog.LevelWarn, "observe kakao room failed", slog.String("error", err.Error()))
	}
}

func (c *Catalog) OpenChat(ctx context.Context, roomID string) bool {
	if c == nil {
		return false
	}

	facts, ok := c.lookup(ctx, roomID)
	return ok && facts.OpenChat()
}

func (c *Catalog) lookup(ctx context.Context, roomID string) (Facts, bool) {
	roomID = normalizeFacts(roomID, "", "").RoomID
	if roomID == "" {
		return Facts{}, false
	}

	if facts, ok := c.cached(roomID); ok {
		return facts, true
	}

	if facts, ok, err := c.store.get(ctx, roomID); err != nil {
		if c.logger != nil {
			c.logger.LogAttrs(ctx, slog.LevelWarn, "load kakao room failed", slog.String("error", err.Error()))
		}
		return Facts{}, false
	} else if ok {
		c.remember(facts)
		return facts, true
	}

	if c.refresh(ctx) {
		if facts, ok := c.cached(roomID); ok {
			return facts, true
		}
	}

	return Facts{}, false
}

func (c *Catalog) refresh(ctx context.Context) bool {
	if c.lister == nil {
		return false
	}

	rooms, err := c.lister.GetRooms(ctx)
	if err != nil {
		if c.logger != nil {
			c.logger.LogAttrs(ctx, slog.LevelWarn, "list kakao rooms failed", slog.String("error", err.Error()))
		}
		return false
	}

	for _, facts := range rooms {
		if facts.RoomID == "" || (facts.RoomType == "" && facts.RoomLinkID == "") {
			continue
		}
		c.remember(facts)
		if err := c.store.upsert(ctx, facts); err != nil && c.logger != nil {
			c.logger.LogAttrs(ctx, slog.LevelWarn, "store kakao room failed", slog.String("error", err.Error()))
		}
	}

	return true
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
