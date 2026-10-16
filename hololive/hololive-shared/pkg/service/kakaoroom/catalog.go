package kakaoroom

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type RoomLister interface {
	GetRooms(ctx context.Context) ([]Facts, error)
}

type roomStore interface {
	upsert(context.Context, Facts) error
	get(context.Context, string) (Facts, bool, error)
}

type Catalog struct {
	store  roomStore
	lister RoomLister
	logger *slog.Logger
	loads  singleflight.Group

	mu    sync.RWMutex
	cache map[string]Facts
}

func New(pool *pgxpool.Pool, lister RoomLister, logger *slog.Logger) *Catalog {
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
	if c.lister == nil || ctx.Err() != nil {
		return false
	}

	// 방 ID와 무관하게 전체 목록 조회와 저장 순회를 함께 공유한다.
	resultCh := c.loads.DoChan("", func() (any, error) {
		return nil, c.refreshRooms(ctx)
	})

	select {
	case <-ctx.Done():
		return false
	case <-resultCh:
		// owner가 저장 중 만료돼도 이미 관측한 방 사실은 유효하다. 살아 있는 대기자는 cache로 판정한다.
		return ctx.Err() == nil
	}
}

func (c *Catalog) refreshRooms(ctx context.Context) error {
	// 첫 대기자의 취소와 분리하되 HTTP와 모든 저장 작업에 하나의 시간 상한을 적용한다.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.QueryTimeout.Default)
	defer cancel()

	rooms, err := c.lister.GetRooms(refreshCtx)
	if err != nil {
		c.warn(refreshCtx, "list kakao rooms failed", err)

		return fmt.Errorf("list kakao rooms: %w", err)
	}

	c.storeListed(refreshCtx, rooms)

	if err := refreshCtx.Err(); err != nil {
		return fmt.Errorf("refresh kakao rooms: %w", err)
	}

	return nil
}

func (c *Catalog) storeListed(ctx context.Context, rooms []Facts) {
	for _, facts := range rooms {
		if ctx.Err() != nil {
			return
		}

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

	c.logger.LogAttrs(ctx, slog.LevelWarn, msg, slog.Any("error", err))
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
