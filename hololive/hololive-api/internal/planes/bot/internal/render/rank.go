package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"sync"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type RankCardEntry struct {
	Rank  int
	Name  string
	Delta string
	Total string
	Photo string
}

const rankCardCacheLimit = 8

type RankCardRenderer struct {
	strings    *messagestrings.Store
	cacheMu    sync.Mutex
	cache      map[string][]byte
	cacheOrder []string
}

type RankCardRendererOption func(*RankCardRenderer)

func WithRankStrings(store *messagestrings.Store) RankCardRendererOption {
	return func(r *RankCardRenderer) {
		r.strings = store
	}
}

func NewRankCardRenderer(options ...RankCardRendererOption) *RankCardRenderer {
	r := &RankCardRenderer{cache: make(map[string][]byte)}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r
}

func (r *RankCardRenderer) RenderRankImage(periodLabel string, entries []RankCardEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, errors.New("rank card: no entries")
	}

	key := rankCardCacheKey(periodLabel, entries)
	if cached, ok := r.cachedRank(key); ok {
		return cached, nil
	}

	rendered, err := r.renderRankImage(periodLabel, entries)
	if err != nil {
		return nil, err
	}
	r.storeCachedRank(key, rendered)
	return rendered, nil
}

func rankCardCacheKey(periodLabel string, entries []RankCardEntry) string {
	hash := sha256.New()
	writeCacheString(hash, periodLabel)
	writeCacheInt(hash, len(entries))
	for _, e := range entries {
		writeCacheInt(hash, e.Rank)
		writeCacheString(hash, e.Name)
		writeCacheString(hash, e.Delta)
		writeCacheString(hash, e.Total)
		writeCacheString(hash, e.Photo)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (r *RankCardRenderer) cachedRank(key string) ([]byte, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	data, ok := r.cache[key]
	if !ok {
		return nil, false
	}
	return bytes.Clone(data), true
}

func (r *RankCardRenderer) storeCachedRank(key string, data []byte) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if _, ok := r.cache[key]; !ok {
		r.cacheOrder = append(r.cacheOrder, key)
	}
	r.cache[key] = bytes.Clone(data)
	for len(r.cacheOrder) > rankCardCacheLimit {
		oldest := r.cacheOrder[0]
		r.cacheOrder = r.cacheOrder[1:]
		delete(r.cache, oldest)
	}
}

type rankMetrics struct {
	calendarMetrics
	rowH, rankCol int
}

func newRankMetrics() rankMetrics {
	base := newCalendarMetrics()
	return rankMetrics{
		calendarMetrics: base,
		rowH:            int(112 * base.sf),
		rankCol:         int(56 * base.sf),
	}
}

func (r *RankCardRenderer) renderRankImage(periodLabel string, entries []RankCardEntry) ([]byte, error) {
	photos := fetchPhotosByURL(rankPhotoURLs(entries))

	fontMu.Lock()
	defer fontMu.Unlock()

	m := newRankMetrics()
	f, err := loadCalendarFonts(m.sf)
	if err != nil {
		return nil, err
	}
	m.fonts = f
	m.strings = r.strings

	height := m.headerH + separatorH + m.paddingY + len(entries)*m.rowH + m.paddingY
	img := cardkit.NewCanvas(canvasWidth, min(height, maxCanvasH), colWhite)

	cardkit.DrawText(img, m.fonts.title, paddingX, int(42*m.sf), colSlate800, m.rankHeaderText())
	cardkit.DrawText(img, m.fonts.stat, paddingX, int(68*m.sf), colSlate500, m.rankSummaryText(periodLabel, len(entries)))
	cardkit.FillRect(img, image.Rect(paddingX, m.headerH, canvasWidth-paddingX, m.headerH+separatorH), colSlate200)

	y := m.headerH + separatorH + m.paddingY
	for _, e := range entries {
		drawRankRow(img, &m, y, e, photos)
		y += m.rowH
	}

	return cardkit.EncodePNG(img, calendarOutputWidth)
}

func rankPhotoURLs(entries []RankCardEntry) []string {
	urls := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Photo != "" {
			urls = append(urls, e.Photo)
		}
	}
	return urls
}

func drawRankRow(img *image.RGBA, m *rankMetrics, y int, e RankCardEntry, photos map[string]image.Image) {
	rankColor := colSlate500
	if e.Rank <= 3 {
		rankColor = colAmber600
	}
	cardkit.DrawCenteredText(img, m.fonts.name, paddingX+m.rankCol/2, y+m.rowH/2+int(8*m.sf), rankColor, fmt.Sprintf("%d", e.Rank))

	avatarX := paddingX + m.rankCol + int(8*m.sf)
	cardkit.AvatarCircle(img, avatarX+m.avatarSize/2, y+m.rowH/2, m.avatarSize/2, photos[e.Photo], e.Name, m.rankAvatarStyle())

	nameX := avatarX + m.avatarSize + m.avatarGap
	deltaW := cardkit.MeasureText(m.fonts.name, e.Delta)
	deltaX := canvasWidth - paddingX - deltaW

	name := cardkit.ClampToWidth(m.fonts.name, cardkit.DropUncoveredRunes(m.fonts.name, e.Name), deltaX-nameX-m.avatarGap)
	cardkit.DrawText(img, m.fonts.name, nameX, y+int(46*m.sf), colSlate800, name)
	cardkit.DrawText(img, m.fonts.name, deltaX, y+int(46*m.sf), colEmerald600, e.Delta)

	if e.Total != "" {
		total := cardkit.ClampToWidth(m.fonts.date, m.rankTotalText(e.Total), canvasWidth-paddingX-nameX)
		cardkit.DrawText(img, m.fonts.date, nameX, y+int(78*m.sf), colSlate500, total)
	}
}

func (m *rankMetrics) rankAvatarStyle() cardkit.AvatarStyle {
	return cardkit.AvatarStyle{
		Ring:        colSlate200,
		RingWidth:   int(m.sf) + 1,
		Accent:      colAmber600,
		Background:  colWhite,
		Initials:    m.fonts.avatar,
		TextColor:   colWhite,
		InitialDrop: int(12 * m.sf),
	}
}

func (m *rankMetrics) rankStr(key, fallback string) string {
	return m.strings.GetOr(messagestrings.NamespaceRankCard, key, fallback)
}

func (m *rankMetrics) rankHeaderText() string {
	return m.rankStr("header", "구독자 증가 순위")
}

func (m *rankMetrics) rankSummaryText(periodLabel string, total int) string {
	return fmt.Sprintf(m.rankStr("summary", "%s · 상위 %d"), periodLabel, total)
}

func (m *rankMetrics) rankTotalText(total string) string {
	return fmt.Sprintf(m.rankStr("total", "구독자 %s"), total)
}
