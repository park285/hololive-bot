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

func (r *CalendarCardRenderer) RenderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	photos := fetchMemberPhotos(entries)

	fontMu.Lock()
	defer fontMu.Unlock()

	grouped := groupEntriesByDay(entries)
	height := calculateCanvasHeight(grouped)
	if height > maxCanvasH {
		height = maxCanvasH
	}

	img := image.NewRGBA(image.Rect(0, 0, canvasWidth, height))
	fillRect(img, img.Bounds(), colWhite)

	sf := float64(scaleFactor)
	titleFace, err := fonts.CaptionFaceSized(24 * sf)
	if err != nil {
		return nil, fmt.Errorf("load title font: %w", err)
	}
	nameFace, err := fonts.CaptionFaceSized(16 * sf)
	if err != nil {
		return nil, fmt.Errorf("load name font: %w", err)
	}
	dateFace, err := fonts.CaptionFaceSized(13 * sf)
	if err != nil {
		return nil, fmt.Errorf("load date font: %w", err)
	}
	badgeFace, err := fonts.CaptionFaceSized(12 * sf)
	if err != nil {
		return nil, fmt.Errorf("load badge font: %w", err)
	}
	statFace, err := fonts.CaptionFaceSized(11 * sf)
	if err != nil {
		return nil, fmt.Errorf("load stat font: %w", err)
	}
	avatarFace, err := fonts.CaptionFaceSized(16 * sf)
	if err != nil {
		return nil, fmt.Errorf("load avatar font: %w", err)
	}

	drawText(img, titleFace, paddingX, int(34*sf), colSlate800, fmt.Sprintf("%d년 %d월 기념일", year, month))

	bc, ac := countByKind(entries)
	statText := fmt.Sprintf("총 %d건 · 생일 %d · 데뷔주년 %d", len(entries), bc, ac)
	drawText(img, statFace, paddingX, int(58*sf), colSlate500, statText)

	sepY := headerH
	fillRect(img, image.Rect(paddingX, sepY, canvasWidth-paddingX, sepY+separatorH), colSlate200)

	y := sepY + separatorH + paddingY

	if len(entries) == 0 {
		drawText(img, nameFace, paddingX, y+int(24*sf), colSlate500, "등록된 기념일이 없습니다.")
	} else {
		for _, group := range grouped {
			if y >= maxCanvasH-paddingY {
				break
			}
			drawText(img, dateFace, paddingX, y+int(18*sf), colSlate500,
				fmt.Sprintf("%d월 %d일", month, group.day))
			fillRect(img, image.Rect(paddingX, y+dateHeaderH-separatorH, canvasWidth-paddingX, y+dateHeaderH), colSlate200)
			y += dateHeaderH

			for _, entry := range group.entries {
				if y >= maxCanvasH-paddingY {
					break
				}
				drawEntryRow(img, nameFace, badgeFace, avatarFace, paddingX+entryIndent, y, entry, photos)
				y += entryRowH
			}
			y += dateSectGap
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode calendar png: %w", err)
	}
	return buf.Bytes(), nil
}

func drawEntryRow(img *image.RGBA, nameFace, badgeFace, avatarFace font.Face, x, y int, entry domain.CalendarEntry, photos map[string]image.Image) {
	name := entryDisplayName(entry.Member)

	var accentCol, badgeBg color.RGBA
	var badgeText string
	switch entry.Kind {
	case domain.CelebrationKindBirthday:
		accentCol = colAmber600
		badgeBg = colAmber50
		badgeText = "생일"
	case domain.CelebrationKindAnniversary:
		accentCol = colEmerald600
		badgeBg = colEmerald50
		badgeText = fmt.Sprintf("데뷔 %d주년", entry.Ordinal)
	default:
		accentCol = colSlate500
		badgeBg = colSlate100
	}

	avatarCx := x + avatarSize/2
	avatarCy := y + entryRowH/2
	avatarR := avatarSize / 2

	ringW := scaleFactor + 1
	fillCircle(img, avatarCx, avatarCy, avatarR+ringW, colSlate200)

	var drewPhoto bool
	if entry.Member != nil && entry.Member.Photo != "" {
		if photo, ok := photos[entry.Member.Photo]; ok {
			drawCircularImage(img, photo, avatarCx, avatarCy, avatarR, colWhite)
			drewPhoto = true
		}
	}
	if !drewPhoto {
		fillCircle(img, avatarCx, avatarCy, avatarR, accentCol)
		initial := firstRune(name)
		if initial != "" {
			iw := measureText(avatarFace, initial)
			drawText(img, avatarFace, avatarCx-iw/2, avatarCy+int(6*float64(scaleFactor)), colWhite, initial)
		}
	}

	nameX := x + avatarSize + avatarGap
	drawText(img, nameFace, nameX, y+entryRowH/2+int(6*float64(scaleFactor)), colSlate800, name)

	if badgeText != "" {
		bw := measureText(badgeFace, badgeText)
		bx := canvasWidth - paddingX - bw - badgePadX*2
		by := y + (entryRowH-badgeH)/2
		fillRoundedRect(img, image.Rect(bx, by, bx+bw+badgePadX*2, by+badgeH), badgeRadius, badgeBg)
		drawText(img, badgeFace, bx+badgePadX, by+badgeH-badgePadY-int(2*float64(scaleFactor)), accentCol, badgeText)
	}
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
		switch e.Kind {
		case domain.CelebrationKindBirthday:
			birthday++
		case domain.CelebrationKindAnniversary:
			anniversary++
		}
	}
	return
}

var photoClient = &http.Client{Timeout: 5 * time.Second}

func fetchMemberPhotos(entries []domain.CalendarEntry) map[string]image.Image {
	photos := make(map[string]image.Image)
	for _, e := range entries {
		if e.Member == nil || e.Member.Photo == "" {
			continue
		}
		key := e.Member.Photo
		if _, ok := photos[key]; ok {
			continue
		}
		url := thumbnailURL(e.Member.Photo, 256)
		if img, err := fetchImage(url); err == nil {
			photos[key] = img
		}
	}
	return photos
}

func thumbnailURL(original string, size int) string {
	if idx := strings.Index(original, "=s"); idx != -1 {
		return fmt.Sprintf("%s=s%d-c-k-c0x00ffffff-no-rj", original[:idx], size)
	}
	return original
}

func fetchImage(url string) (image.Image, error) {
	resp, err := photoClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
