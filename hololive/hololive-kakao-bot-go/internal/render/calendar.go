package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-kakao-bot-go/internal/assets/fonts"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var fontMu sync.Mutex

type CalendarCardRenderer struct{}

func NewCalendarCardRenderer() *CalendarCardRenderer {
	return &CalendarCardRenderer{}
}

type calendarFonts struct {
	title, name, date, badge, stat, avatar font.Face
}

func loadCalendarFonts() (calendarFonts, error) {
	sf := float64(scaleFactor)
	var f calendarFonts
	var err error
	if f.title, err = fonts.CaptionFaceSized(24 * sf); err != nil {
		return f, fmt.Errorf("load title font: %w", err)
	}
	if f.name, err = fonts.CaptionFaceSized(16 * sf); err != nil {
		return f, fmt.Errorf("load name font: %w", err)
	}
	if f.date, err = fonts.CaptionFaceSized(13 * sf); err != nil {
		return f, fmt.Errorf("load date font: %w", err)
	}
	if f.badge, err = fonts.CaptionFaceSized(12 * sf); err != nil {
		return f, fmt.Errorf("load badge font: %w", err)
	}
	if f.stat, err = fonts.CaptionFaceSized(11 * sf); err != nil {
		return f, fmt.Errorf("load stat font: %w", err)
	}
	if f.avatar, err = fonts.CaptionFaceSized(16 * sf); err != nil {
		return f, fmt.Errorf("load avatar font: %w", err)
	}
	return f, nil
}

func (r *CalendarCardRenderer) RenderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	photos := fetchMemberPhotos(entries)

	fontMu.Lock()
	defer fontMu.Unlock()

	f, err := loadCalendarFonts()
	if err != nil {
		return nil, err
	}

	grouped := groupEntriesByDay(entries)
	height := min(calculateCanvasHeight(grouped), maxCanvasH)

	img := image.NewRGBA(image.Rect(0, 0, canvasWidth, height))
	fillRect(img, img.Bounds(), colWhite)

	drawCalendarHeader(img, f, month, year, entries)
	drawCalendarBody(img, f, month, grouped, photos)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode calendar png: %w", err)
	}
	return buf.Bytes(), nil
}

func drawCalendarHeader(img *image.RGBA, f calendarFonts, month, year int, entries []domain.CalendarEntry) {
	sf := float64(scaleFactor)
	drawText(img, f.title, paddingX, int(34*sf), colSlate800, fmt.Sprintf("%d년 %d월 기념일", year, month))

	bc, ac := countByKind(entries)
	statText := fmt.Sprintf("총 %d건 · 생일 %d · 데뷔주년 %d", len(entries), bc, ac)
	drawText(img, f.stat, paddingX, int(58*sf), colSlate500, statText)

	fillRect(img, image.Rect(paddingX, headerH, canvasWidth-paddingX, headerH+separatorH), colSlate200)
}

func drawCalendarBody(img *image.RGBA, f calendarFonts, month int, grouped []dayGroup, photos map[string]image.Image) {
	y := headerH + separatorH + paddingY

	if len(grouped) == 0 {
		drawText(img, f.name, paddingX, y+int(24*float64(scaleFactor)), colSlate500, "등록된 기념일이 없습니다.")
		return
	}

	for _, group := range grouped {
		if y >= maxCanvasH-paddingY {
			break
		}
		y = drawDayGroup(img, f, month, group, y, photos)
	}
}

func drawDayGroup(img *image.RGBA, f calendarFonts, month int, group dayGroup, y int, photos map[string]image.Image) int {
	sf := float64(scaleFactor)
	drawText(img, f.date, paddingX, y+int(18*sf), colSlate500, fmt.Sprintf("%d월 %d일", month, group.day))
	fillRect(img, image.Rect(paddingX, y+dateHeaderH-separatorH, canvasWidth-paddingX, y+dateHeaderH), colSlate200)
	y += dateHeaderH

	for _, entry := range group.entries {
		if y >= maxCanvasH-paddingY {
			break
		}
		drawEntryRow(img, f, paddingX+entryIndent, y, entry, photos)
		y += entryRowH
	}
	return y + dateSectGap
}

type entryStyle struct {
	accent, badgeBg color.RGBA
	badgeText       string
}

func resolveEntryStyle(entry domain.CalendarEntry) entryStyle {
	switch entry.Kind {
	case domain.CelebrationKindBirthday:
		return entryStyle{colAmber600, colAmber50, "생일"}
	case domain.CelebrationKindAnniversary:
		return entryStyle{colEmerald600, colEmerald50, fmt.Sprintf("데뷔 %d주년", entry.Ordinal)}
	default:
		return entryStyle{colSlate500, colSlate100, ""}
	}
}

func drawEntryRow(img *image.RGBA, f calendarFonts, x, y int, entry domain.CalendarEntry, photos map[string]image.Image) {
	name := entryDisplayName(entry.Member)
	style := resolveEntryStyle(entry)

	drawEntryAvatar(img, f.avatar, x, y, entry, style.accent, name, photos)

	nameX := x + avatarSize + avatarGap
	drawText(img, f.name, nameX, y+entryRowH/2+int(6*float64(scaleFactor)), colSlate800, name)

	if style.badgeText != "" {
		drawEntryBadge(img, f.badge, y, style)
	}
}

func drawEntryAvatar(img *image.RGBA, avatarFace font.Face, x, y int, entry domain.CalendarEntry, accent color.RGBA, name string, photos map[string]image.Image) {
	cx := x + avatarSize/2
	cy := y + entryRowH/2
	r := avatarSize / 2

	fillCircle(img, cx, cy, r+scaleFactor+1, colSlate200)

	if entry.Member != nil && entry.Member.Photo != "" {
		if photo, ok := photos[entry.Member.Photo]; ok {
			drawCircularImage(img, photo, cx, cy, r, colWhite)
			return
		}
	}

	fillCircle(img, cx, cy, r, accent)
	initial := firstRune(name)
	if initial != "" {
		iw := measureText(avatarFace, initial)
		drawText(img, avatarFace, cx-iw/2, cy+int(6*float64(scaleFactor)), colWhite, initial)
	}
}

func drawEntryBadge(img *image.RGBA, badgeFace font.Face, y int, s entryStyle) {
	bw := measureText(badgeFace, s.badgeText)
	bx := canvasWidth - paddingX - bw - badgePadX*2
	by := y + (entryRowH-badgeH)/2
	fillRoundedRect(img, image.Rect(bx, by, bx+bw+badgePadX*2, by+badgeH), badgeRadius, s.badgeBg)
	drawText(img, badgeFace, bx+badgePadX, by+badgeH-badgePadY-int(2*float64(scaleFactor)), s.accent, s.badgeText)
}

func firstRune(s string) string {
	if s == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return ""
	}
	return string(r)
}

func countByKind(entries []domain.CalendarEntry) (birthday, anniversary int) {
	for _, e := range entries {
		addKindCount(e.Kind, &birthday, &anniversary)
	}
	return
}

func addKindCount(kind domain.CelebrationKind, birthday, anniversary *int) {
	switch kind {
	case domain.CelebrationKindBirthday:
		(*birthday)++
	case domain.CelebrationKindAnniversary:
		(*anniversary)++
	}
}

var photoClient = &http.Client{Timeout: 5 * time.Second}

const photoFetchBudget = 15 * time.Second

func fetchMemberPhotos(entries []domain.CalendarEntry) map[string]image.Image {
	deadline := time.Now().Add(photoFetchBudget)
	photos := make(map[string]image.Image)
	for _, e := range entries {
		if time.Now().After(deadline) {
			break
		}
		fetchMemberPhoto(e, photos)
	}
	return photos
}

func fetchMemberPhoto(e domain.CalendarEntry, photos map[string]image.Image) {
	if e.Member == nil || e.Member.Photo == "" {
		return
	}
	if _, ok := photos[e.Member.Photo]; ok {
		return
	}
	url := thumbnailURL(e.Member.Photo, 256)
	if img, err := fetchImage(url); err == nil {
		photos[e.Member.Photo] = img
	}
}

func thumbnailURL(original string, size int) string {
	if before, _, ok := strings.Cut(original, "=s"); ok {
		return fmt.Sprintf("%s=s%d-c-k-c0x00ffffff-no-rj", before, size)
	}
	return original
}

func fetchImage(url string) (image.Image, error) {
	resp, err := photoClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	if img, err := png.Decode(bytes.NewReader(data)); err == nil {
		return img, nil
	}
	if img, err := jpeg.Decode(bytes.NewReader(data)); err == nil {
		return img, nil
	}
	return nil, fmt.Errorf("unsupported image format")
}

type dayGroup struct {
	day     int
	entries []domain.CalendarEntry
}

func groupEntriesByDay(entries []domain.CalendarEntry) []dayGroup {
	var groups []dayGroup
	var current *dayGroup
	for _, e := range entries {
		if current == nil || current.day != e.Day {
			if current != nil {
				groups = append(groups, *current)
			}
			current = &dayGroup{day: e.Day}
		}
		current.entries = append(current.entries, e)
	}
	if current != nil {
		groups = append(groups, *current)
	}
	return groups
}

func calculateCanvasHeight(groups []dayGroup) int {
	h := headerH + separatorH + paddingY
	if len(groups) == 0 {
		return h + 60*scaleFactor + paddingY
	}
	for _, g := range groups {
		h += dateHeaderH + len(g.entries)*entryRowH + dateSectGap
	}
	return h + paddingY
}

func entryDisplayName(m *domain.Member) string {
	if m == nil {
		return "알 수 없음"
	}
	if m.ShortKoreanName != "" {
		return m.ShortKoreanName
	}
	if m.NameKo != "" {
		return m.NameKo
	}
	return m.Name
}
