package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"strings"
	"sync"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
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
	data := baseProfileCardData(member, raw)
	applyProfileTranslation(&data, translated)
	return data
}

func baseProfileCardData(member *domain.Member, raw *domain.TalentProfile) ProfileCardData {
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
	return data
}

func applyProfileTranslation(data *ProfileCardData, translated *domain.Translated) {
	if translated == nil {
		return
	}
	applyTranslatedDisplayName(data, strings.TrimSpace(translated.DisplayName))
	if catch := strings.TrimSpace(translated.Catchphrase); catch != "" {
		data.Catchphrase = catch
	}
	if len(translated.Data) > 0 {
		data.Rows = translatedProfileRows(translated.Data)
	}
}

func applyTranslatedDisplayName(data *ProfileCardData, display string) {
	if display == "" {
		return
	}
	if data.DisplayName != "" && display != data.DisplayName {
		data.SubNames = append([]string{data.DisplayName}, data.SubNames...)
	}
	data.DisplayName = display
}

func translatedProfileRows(translated []domain.TranslatedProfileDataRow) []ProfileCardRow {
	rows := make([]ProfileCardRow, 0, len(translated))
	for _, row := range translated {
		rows = appendProfileRow(rows, row.Label, row.Value)
	}
	return rows
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

func (r *ProfileCardRenderer) RenderProfileImage(data *ProfileCardData) ([]byte, error) {
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

func profileCardCacheKey(data *ProfileCardData) string {
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

func (r *ProfileCardRenderer) renderProfileImage(data *ProfileCardData) ([]byte, error) {
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

	img := cardkit.NewCanvas(canvasWidth, min(profileCardHeight(&m, data), maxCanvasH), colWhite)

	y := drawProfileIdentity(img, &m, data, photos)
	drawProfileRows(img, &m, data.Rows, y)
	if data.Graduated {
		cardkit.BadgeRightAligned(img, canvasWidth-paddingX, int(30*m.sf), m.profileGraduatedBadge(), m.graduatedBadgeStyle())
	}

	return cardkit.EncodePNG(img, calendarOutputWidth)
}

func (m *profileMetrics) graduatedBadgeStyle() cardkit.BadgeStyle {
	return cardkit.BadgeStyle{
		Face:         m.fonts.badge,
		Background:   colAmber50,
		Text:         colAmber600,
		PadX:         m.badgePadX,
		PadY:         m.badgePadY,
		Height:       m.badgeH,
		Radius:       m.badgeRadius,
		BaselineLift: int(2 * m.sf),
	}
}

func profileCardHeight(m *profileMetrics, data *ProfileCardData) int {
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

func drawProfileIdentity(img *image.RGBA, m *profileMetrics, data *ProfileCardData, photos map[string]image.Image) int {
	cx := canvasWidth / 2
	cy := int(30*m.sf) + m.avatarR
	cardkit.AvatarCircle(img, cx, cy, m.avatarR, photos[data.Photo], data.DisplayName, m.profileAvatarStyle())

	y := cy + m.avatarR + int(45*m.sf)
	cardkit.DrawCenteredText(img, m.fonts.title, cx, y, colSlate800, cardkit.DropUncoveredRunes(m.fonts.title, data.DisplayName))

	if len(data.SubNames) > 0 {
		y += int(30 * m.sf)
		sub := cardkit.DropUncoveredRunes(m.fonts.stat, strings.Join(data.SubNames, " · "))
		cardkit.DrawCenteredText(img, m.fonts.stat, cx, y, colSlate500, cardkit.ClampToWidth(m.fonts.stat, sub, canvasWidth-paddingX*2))
	}
	if data.Catchphrase != "" {
		y += int(36 * m.sf)
		catch := cardkit.DropUncoveredRunes(m.fonts.date, data.Catchphrase)
		cardkit.DrawCenteredText(img, m.fonts.date, cx, y, colAmber600, cardkit.ClampToWidth(m.fonts.date, catch, canvasWidth-paddingX*2))
	}

	y += int(26 * m.sf)
	cardkit.FillRect(img, image.Rect(paddingX, y-separatorH, canvasWidth-paddingX, y), colSlate200)
	return y
}

func (m *profileMetrics) profileAvatarStyle() cardkit.AvatarStyle {
	return cardkit.AvatarStyle{
		Ring:        colSlate200,
		RingWidth:   int(m.sf) + 1,
		Accent:      colAmber600,
		Background:  colWhite,
		Initials:    m.fonts.title,
		TextColor:   colWhite,
		InitialDrop: int(12 * m.sf),
	}
}

func drawProfileRows(img *image.RGBA, m *profileMetrics, rows []ProfileCardRow, y int) {
	for _, row := range rows {
		baseline := y + int(30*m.sf)
		label := cardkit.ClampToWidth(m.fonts.date, cardkit.DropUncoveredRunes(m.fonts.date, row.Label), m.labelCol-int(12*m.sf))
		cardkit.DrawText(img, m.fonts.date, paddingX, baseline, colSlate500, label)

		valueX := paddingX + m.labelCol
		value := cardkit.ClampToWidth(m.fonts.name, cardkit.DropUncoveredRunes(m.fonts.name, row.Value), canvasWidth-paddingX-valueX)
		cardkit.DrawText(img, m.fonts.name, valueX, baseline, colSlate800, value)
		y += m.rowH
	}
}

func (m *profileMetrics) profileGraduatedBadge() string {
	return m.strings.GetOr(messagestrings.NamespaceProfileCard, "badge_graduated", "졸업")
}
