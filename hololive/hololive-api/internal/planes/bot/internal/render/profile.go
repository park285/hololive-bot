package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type ProfileCardRow struct {
	Label string
	Value string
}

type ProfileCardData struct {
	DisplayName string
	SubNames    []string
	Catchphrase string
	Rows        []ProfileCardRow
	Photo       string
	Graduated   bool
}

func NewProfileCardData(member *domain.Member, raw *domain.TalentProfile, translated *domain.Translated) ProfileCardData {
	data := ProfileCardData{}
	if member != nil {
		data.Photo = member.Photo
		data.Graduated = member.IsGraduated
	}
	if raw != nil {
		data.DisplayName = strings.TrimSpace(raw.EnglishName)
		data.Catchphrase = strings.TrimSpace(raw.Catchphrase)
		data.SubNames = appendNonEmpty(data.SubNames, strings.TrimSpace(raw.JapaneseName))
		for _, entry := range raw.DataEntries {
			data.Rows = appendProfileRow(data.Rows, entry.Label, entry.Value)
		}
	}
	if translated == nil {
		return data
	}
	if display := strings.TrimSpace(translated.DisplayName); display != "" {
		if data.DisplayName != "" && display != data.DisplayName {
			data.SubNames = append([]string{data.DisplayName}, data.SubNames...)
		}
		data.DisplayName = display
	}
	if catch := strings.TrimSpace(translated.Catchphrase); catch != "" {
		data.Catchphrase = catch
	}
	if len(translated.Data) > 0 {
		data.Rows = nil
		for _, row := range translated.Data {
			data.Rows = appendProfileRow(data.Rows, row.Label, row.Value)
		}
	}
	return data
}

func appendNonEmpty(names []string, name string) []string {
	if name == "" {
		return names
	}
	return append(names, name)
}

func appendProfileRow(rows []ProfileCardRow, label, value string) []ProfileCardRow {
	label, value = strings.TrimSpace(label), strings.TrimSpace(value)
	if label == "" || value == "" {
		return rows
	}
	return append(rows, ProfileCardRow{Label: label, Value: value})
}

const profileCardCacheLimit = 24

type ProfileCardRenderer struct {
	strings    *messagestrings.Store
	cacheMu    sync.Mutex
	cache      map[string][]byte
	cacheOrder []string
}

type ProfileCardRendererOption func(*ProfileCardRenderer)

func WithProfileStrings(store *messagestrings.Store) ProfileCardRendererOption {
	return func(r *ProfileCardRenderer) {
		r.strings = store
	}
}

func NewProfileCardRenderer(options ...ProfileCardRendererOption) *ProfileCardRenderer {
	r := &ProfileCardRenderer{cache: make(map[string][]byte)}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r
}

func (r *ProfileCardRenderer) RenderProfileImage(data ProfileCardData) ([]byte, error) {
	if data.DisplayName == "" {
		return nil, errors.New("profile card: display name is empty")
	}

	key := profileCardCacheKey(data)
	if cached, ok := r.cachedProfile(key); ok {
		return cached, nil
	}

	rendered, err := r.renderProfileImage(data)
	if err != nil {
		return nil, err
	}
	r.storeCachedProfile(key, rendered)
	return rendered, nil
}

func profileCardCacheKey(data ProfileCardData) string {
	hash := sha256.New()
	writeCacheString(hash, data.DisplayName)
	writeCacheString(hash, data.Catchphrase)
	writeCacheString(hash, data.Photo)
	writeCacheInt(hash, boolCacheInt(data.Graduated))
	writeCacheInt(hash, len(data.SubNames))
	for _, name := range data.SubNames {
		writeCacheString(hash, name)
	}
	writeCacheInt(hash, len(data.Rows))
	for _, row := range data.Rows {
		writeCacheString(hash, row.Label)
		writeCacheString(hash, row.Value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func boolCacheInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (r *ProfileCardRenderer) cachedProfile(key string) ([]byte, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	data, ok := r.cache[key]
	if !ok {
		return nil, false
	}
	return bytes.Clone(data), true
}

func (r *ProfileCardRenderer) storeCachedProfile(key string, data []byte) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if _, ok := r.cache[key]; !ok {
		r.cacheOrder = append(r.cacheOrder, key)
	}
	r.cache[key] = bytes.Clone(data)
	for len(r.cacheOrder) > profileCardCacheLimit {
		oldest := r.cacheOrder[0]
		r.cacheOrder = r.cacheOrder[1:]
		delete(r.cache, oldest)
	}
}

type profileMetrics struct {
	calendarMetrics
	avatarR, rowH, labelCol int
}

func newProfileMetrics() profileMetrics {
	base := newCalendarMetrics()
	return profileMetrics{
		calendarMetrics: base,
		avatarR:         int(70 * base.sf),
		rowH:            int(44 * base.sf),
		labelCol:        int(180 * base.sf),
	}
}

func (r *ProfileCardRenderer) renderProfileImage(data ProfileCardData) ([]byte, error) {
	photos := fetchPhotosByURL([]string{data.Photo})

	fontMu.Lock()
	defer fontMu.Unlock()

	m := newProfileMetrics()
	f, err := loadCalendarFonts(m.sf)
	if err != nil {
		return nil, err
	}
	m.fonts = f
	m.strings = r.strings

	height := profileCardHeight(&m, data)
	img := image.NewRGBA(image.Rect(0, 0, canvasWidth, min(height, maxCanvasH)))
	fillRect(img, img.Bounds(), colWhite)

	y := drawProfileIdentity(img, &m, data, photos)
	drawProfileRows(img, &m, data.Rows, y)
	if data.Graduated {
		drawProfileGraduatedBadge(img, &m)
	}

	out := downscaleToOutputWidth(img)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode profile png: %w", err)
	}
	return buf.Bytes(), nil
}

func profileCardHeight(m *profileMetrics, data ProfileCardData) int {
	h := int(30*m.sf) + m.avatarR*2 + int(50*m.sf)
	if len(data.SubNames) > 0 {
		h += int(30 * m.sf)
	}
	if data.Catchphrase != "" {
		h += int(36 * m.sf)
	}
	h += int(26 * m.sf)
	h += len(data.Rows) * m.rowH
	return h + m.paddingY + int(10*m.sf)
}

func drawProfileIdentity(img *image.RGBA, m *profileMetrics, data ProfileCardData, photos map[string]image.Image) int {
	cx := canvasWidth / 2
	cy := int(30*m.sf) + m.avatarR
	drawProfileAvatar(img, m, cx, cy, data, photos)

	y := cy + m.avatarR + int(45*m.sf)
	drawCenteredText(img, m.fonts.title, cx, y, colSlate800, dropUncoveredRunes(m.fonts.title, data.DisplayName))

	if len(data.SubNames) > 0 {
		y += int(30 * m.sf)
		sub := dropUncoveredRunes(m.fonts.stat, strings.Join(data.SubNames, " · "))
		drawCenteredText(img, m.fonts.stat, cx, y, colSlate500, clampToWidth(m.fonts.stat, sub, canvasWidth-paddingX*2))
	}
	if data.Catchphrase != "" {
		y += int(36 * m.sf)
		catch := dropUncoveredRunes(m.fonts.date, data.Catchphrase)
		drawCenteredText(img, m.fonts.date, cx, y, colAmber600, clampToWidth(m.fonts.date, catch, canvasWidth-paddingX*2))
	}

	y += int(26 * m.sf)
	fillRect(img, image.Rect(paddingX, y-separatorH, canvasWidth-paddingX, y), colSlate200)
	return y
}

func drawProfileAvatar(img *image.RGBA, m *profileMetrics, cx, cy int, data ProfileCardData, photos map[string]image.Image) {
	fillCircle(img, cx, cy, m.avatarR+int(m.sf)+1, colSlate200)
	if data.Photo != "" {
		if photo, ok := photos[data.Photo]; ok {
			drawCircularImage(img, photo, cx, cy, m.avatarR, colWhite)
			return
		}
	}
	fillCircle(img, cx, cy, m.avatarR, colAmber600)
	initial := firstRune(dropUncoveredRunes(m.fonts.title, data.DisplayName))
	if initial != "" {
		iw := measureText(m.fonts.title, initial)
		drawText(img, m.fonts.title, cx-iw/2, cy+int(12*m.sf), colWhite, initial)
	}
}

func drawProfileRows(img *image.RGBA, m *profileMetrics, rows []ProfileCardRow, y int) {
	for _, row := range rows {
		baseline := y + int(30*m.sf)
		label := clampToWidth(m.fonts.date, dropUncoveredRunes(m.fonts.date, row.Label), m.labelCol-int(12*m.sf))
		drawText(img, m.fonts.date, paddingX, baseline, colSlate500, label)

		valueX := paddingX + m.labelCol
		value := clampToWidth(m.fonts.name, dropUncoveredRunes(m.fonts.name, row.Value), canvasWidth-paddingX-valueX)
		drawText(img, m.fonts.name, valueX, baseline, colSlate800, value)
		y += m.rowH
	}
}

func drawProfileGraduatedBadge(img *image.RGBA, m *profileMetrics) {
	text := m.profileGraduatedBadge()
	bw := measureText(m.fonts.badge, text)
	bx := canvasWidth - paddingX - bw - m.badgePadX*2
	by := int(30 * m.sf)
	fillRoundedRect(img, image.Rect(bx, by, bx+bw+m.badgePadX*2, by+m.badgeH), m.badgeRadius, colAmber50)
	drawText(img, m.fonts.badge, bx+m.badgePadX, by+m.badgeH-m.badgePadY-int(2*m.sf), colAmber600, text)
}

func drawCenteredText(img *image.RGBA, face font.Face, cx, y int, col color.Color, text string) {
	w := measureText(face, text)
	drawText(img, face, cx-w/2, y, col, text)
}

func (m *profileMetrics) profileGraduatedBadge() string {
	return m.strings.GetOr(messagestrings.NamespaceProfileCard, "badge_graduated", "졸업")
}
