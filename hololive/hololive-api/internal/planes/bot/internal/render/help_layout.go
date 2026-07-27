package render

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
)

const (
	helpCardCanvasWidth      = 1448
	helpCardCanvasHeight     = 1086
	helpCardOutputWidth      = 1448
	helpCardTargetContentH   = 700
	helpCardMaxContentH      = 720
	helpCardCommandWidth     = 350
	helpCardDescriptionWidth = 820
	helpCardMaxSourceLines   = 96
	helpCardMaxLineRunes     = 512
	helpCardMaxWrappedLines  = 2
	helpCardMaxSections      = 24
	helpCardMaxRows          = 64
	helpCardSectionHeight    = 40
	helpCardRowMinHeight     = 58
	helpCardTextLineHeight   = 27
)

type helpCardFonts struct {
	title       font.Face
	subtitle    font.Face
	header      font.Face
	section     font.Face
	command     font.Face
	description font.Face
	footer      font.Face
}

type helpCardDocument struct {
	title    string
	sections []helpCardSection
}

type helpCardSection struct {
	label  string
	rows   []helpCardRow
	height int
}

type helpCardRow struct {
	commandLines     []string
	descriptionLines []string
	height           int
}

type helpCardPage struct {
	title         string
	subtitle      string
	sections      []helpCardSection
	contentHeight int
}

func loadHelpCardFonts() (helpCardFonts, error) {
	load := func(name string, size float64, bold bool) (font.Face, error) {
		var (
			face font.Face
			err  error
		)
		if bold {
			face, err = fonts.CaptionBoldFaceSized(size)
		} else {
			face, err = fonts.CaptionFaceSized(size)
		}
		if err != nil {
			return nil, fmt.Errorf("load help %s font: %w", name, err)
		}
		return face, nil
	}

	var faces helpCardFonts
	var err error
	if faces.title, err = load("title", 48, true); err != nil {
		return faces, err
	}
	if faces.subtitle, err = load("subtitle", 23, false); err != nil {
		return faces, err
	}
	if faces.header, err = load("header", 23, true); err != nil {
		return faces, err
	}
	if faces.section, err = load("section", 22, true); err != nil {
		return faces, err
	}
	if faces.command, err = load("command", 25, true); err != nil {
		return faces, err
	}
	if faces.description, err = load("description", 22, false); err != nil {
		return faces, err
	}
	if faces.footer, err = load("footer", 19, false); err != nil {
		return faces, err
	}
	return faces, nil
}

func parseHelpCardDocument(ctx context.Context, text string, faces helpCardFonts) (helpCardDocument, error) {
	source := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(source) == 0 || len(source) > helpCardMaxSourceLines {
		return helpCardDocument{}, fmt.Errorf("help source line count %d is outside 1..%d", len(source), helpCardMaxSourceLines)
	}

	title := renderableHelpText(faces.title, strings.TrimSpace(source[0]))
	title = cardkit.ClampToWidth(faces.title, title, helpCardCanvasWidth-320)
	if title == "" {
		return helpCardDocument{}, fmt.Errorf("help title cannot be rendered")
	}

	sections, err := parseHelpSections(ctx, source[1:], faces)
	if err != nil {
		return helpCardDocument{}, err
	}
	return helpCardDocument{title: title, sections: sections}, nil
}

func parseHelpSections(ctx context.Context, source []string, faces helpCardFonts) ([]helpCardSection, error) {
	sections := make([]helpCardSection, 0, 8)
	rowCount := 0
	for _, raw := range source {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if label, ok := helpSectionLabel(trimmed); ok {
			if len(sections) >= helpCardMaxSections {
				return nil, fmt.Errorf("help section count exceeds %d", helpCardMaxSections)
			}
			label = renderableHelpText(faces.section, label)
			if label == "" {
				return nil, fmt.Errorf("help section cannot be rendered")
			}
			sections = append(sections, helpCardSection{label: label, height: helpCardSectionHeight})
			continue
		}
		if len(sections) == 0 {
			sections = append(sections, helpCardSection{label: "명령어", height: helpCardSectionHeight})
		}
		if rowCount >= helpCardMaxRows {
			return nil, fmt.Errorf("help row count exceeds %d", helpCardMaxRows)
		}
		row, err := layoutHelpRow(trimmed, faces)
		if err != nil {
			return nil, err
		}
		last := len(sections) - 1
		sections[last].rows = append(sections[last].rows, row)
		sections[last].height += row.height
		rowCount++
	}
	if len(sections) == 0 || rowCount == 0 {
		return nil, fmt.Errorf("help card has no renderable commands")
	}
	for _, section := range sections {
		if len(section.rows) == 0 {
			return nil, fmt.Errorf("help section %q has no commands", section.label)
		}
	}
	return sections, nil
}

func helpSectionLabel(line string) (string, bool) {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return strings.TrimSpace(line[1 : len(line)-1]), true
	}
	return "", false
}

func layoutHelpRow(line string, faces helpCardFonts) (helpCardRow, error) {
	if utf8.RuneCountInString(line) > helpCardMaxLineRunes {
		return helpCardRow{}, fmt.Errorf("help line exceeds %d runes", helpCardMaxLineRunes)
	}
	command, description := splitHelpColumns(line)
	command = renderableHelpText(faces.command, command)
	description = renderableHelpText(faces.description, description)
	commandLines := wrapHelpLine(faces.command, command, helpCardCommandWidth)
	descriptionLines := wrapHelpLine(faces.description, description, helpCardDescriptionWidth)
	if len(commandLines) == 0 {
		return helpCardRow{}, fmt.Errorf("help command cannot be rendered")
	}
	if len(commandLines) > helpCardMaxWrappedLines || len(descriptionLines) > helpCardMaxWrappedLines {
		return helpCardRow{}, fmt.Errorf("help row exceeds %d wrapped lines", helpCardMaxWrappedLines)
	}

	lineCount := max(len(commandLines), len(descriptionLines))
	if lineCount == 0 {
		lineCount = 1
	}
	return helpCardRow{
		commandLines:     commandLines,
		descriptionLines: descriptionLines,
		height:           max(helpCardRowMinHeight, 18+lineCount*helpCardTextLineHeight),
	}, nil
}

func splitHelpColumns(line string) (string, string) {
	if command, description, ok := strings.Cut(line, " - "); ok {
		return strings.TrimSpace(command), strings.TrimSpace(description)
	}
	if label, description, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(strings.TrimSpace(label), "!") {
		return strings.TrimSpace(label), strings.TrimSpace(description)
	}
	return strings.TrimSpace(line), ""
}

func paginateHelpDocument(document helpCardDocument) ([]helpCardPage, error) {
	pages := make([]helpCardPage, 0, 3)
	current := make([]helpCardSection, 0, 4)
	currentHeight := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		pages = append(pages, newHelpCardPage(document.title, current, currentHeight))
		current = nil
		currentHeight = 0
	}

	for _, section := range document.sections {
		if section.height > helpCardMaxContentH {
			flush()
			fragments, err := splitHelpSection(section)
			if err != nil {
				return nil, err
			}
			for _, fragment := range fragments {
				pages = append(pages, newHelpCardPage(document.title, []helpCardSection{fragment}, fragment.height))
			}
			continue
		}
		if len(current) > 0 && currentHeight+section.height > helpCardTargetContentH {
			flush()
		}
		current = append(current, section)
		currentHeight += section.height
	}
	flush()

	if len(pages) == 0 || len(pages) > helpCardMaxImages {
		return nil, fmt.Errorf("help page count %d is outside 1..%d", len(pages), helpCardMaxImages)
	}
	for index := range pages {
		pages[index].subtitle = helpPageSubtitle(pages[index].sections, index+1, len(pages))
	}
	return pages, nil
}

func splitHelpSection(section helpCardSection) ([]helpCardSection, error) {
	fragments := make([]helpCardSection, 0, 2)
	rows := section.rows
	continued := false
	for len(rows) > 0 {
		label := section.label
		if continued {
			label += " · 계속"
		}
		fragment := helpCardSection{label: label, height: helpCardSectionHeight}
		for len(rows) > 0 && fragment.height+rows[0].height <= helpCardMaxContentH {
			fragment.rows = append(fragment.rows, rows[0])
			fragment.height += rows[0].height
			rows = rows[1:]
		}
		if len(fragment.rows) == 0 {
			return nil, fmt.Errorf("help section %q contains a row larger than the page budget", section.label)
		}
		fragments = append(fragments, fragment)
		continued = true
	}
	return fragments, nil
}

func newHelpCardPage(title string, sections []helpCardSection, contentHeight int) helpCardPage {
	return helpCardPage{
		title:         title,
		sections:      append([]helpCardSection(nil), sections...),
		contentHeight: contentHeight,
	}
}

func helpPageSubtitle(sections []helpCardSection, page, total int) string {
	labels := make([]string, 0, len(sections))
	for _, section := range sections {
		label := strings.TrimSuffix(section.label, " · 계속")
		if len(labels) == 0 || labels[len(labels)-1] != label {
			labels = append(labels, label)
		}
	}
	subtitle := strings.Join(labels, " · ")
	if total > 1 {
		subtitle = fmt.Sprintf("%s · %d/%d", subtitle, page, total)
	}
	return subtitle
}

func renderableHelpText(face font.Face, text string) string {
	return cardkit.DropUncoveredRunes(face, strings.TrimSpace(text))
}

func wrapHelpLine(face font.Face, text string, maxWidth int) []string {
	if text == "" {
		return nil
	}
	if cardkit.MeasureText(face, text) <= maxWidth {
		return []string{text}
	}

	runes := []rune(text)
	lines := make([]string, 0, 2)
	for len(runes) > 0 {
		end := fittingHelpPrefix(face, runes, maxWidth)
		if end < len(runes) {
			end = preferredHelpBreak(runes, end)
		}
		line := strings.TrimSpace(string(runes[:end]))
		if line != "" {
			lines = append(lines, line)
		}
		runes = runes[end:]
		for len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	return lines
}

func fittingHelpPrefix(face font.Face, runes []rune, maxWidth int) int {
	low, high := 1, len(runes)
	for low < high {
		mid := low + (high-low+1)/2
		if cardkit.MeasureText(face, strings.TrimSpace(string(runes[:mid]))) <= maxWidth {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}

func preferredHelpBreak(runes []rune, fitting int) int {
	for index := fitting - 1; index > 0; index-- {
		if runes[index] == ' ' {
			return index
		}
	}
	return fitting
}
