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
	helpCardTargetContentH   = 720
	helpCardMaxContentH      = 744
	helpCardCommandWidth     = 350
	helpCardDescriptionWidth = 820
	helpCardMaxSourceLines   = 96
	helpCardMaxLineRunes     = 512
	helpCardMaxWrappedLines  = 3
	helpCardMaxSections      = 24
	helpCardMaxRows          = 64
	helpCardSectionHeight    = 36
	helpCardRowMinHeight     = 52
	helpCardTextLineHeight   = 24
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
	var faces helpCardFonts
	specs := []struct {
		target *font.Face
		name   string
		size   float64
		bold   bool
	}{
		{&faces.title, "title", 48, true},
		{&faces.subtitle, "subtitle", 23, false},
		{&faces.header, "header", 23, true},
		{&faces.section, "section", 22, true},
		{&faces.command, "command", 25, true},
		{&faces.description, "description", 22, false},
		{&faces.footer, "footer", 19, false},
	}
	for _, spec := range specs {
		face, err := loadHelpCardFace(spec.name, spec.size, spec.bold)
		if err != nil {
			return faces, err
		}
		*spec.target = face
	}
	return faces, nil
}

func loadHelpCardFace(name string, size float64, bold bool) (font.Face, error) {
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

func parseHelpCardDocument(ctx context.Context, text string, faces *helpCardFonts) (helpCardDocument, error) {
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

func parseHelpSections(ctx context.Context, source []string, faces *helpCardFonts) ([]helpCardSection, error) {
	builder := helpSectionBuilder{faces: faces, sections: make([]helpCardSection, 0, 8)}
	for _, raw := range source {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if err := builder.addLine(trimmed); err != nil {
			return nil, err
		}
	}
	return builder.build()
}

type helpSectionBuilder struct {
	faces    *helpCardFonts
	sections []helpCardSection
	rowCount int
}

func (b *helpSectionBuilder) addLine(line string) error {
	if label, ok := helpSectionLabel(line); ok {
		return b.appendSection(label)
	}
	return b.appendRow(line)
}

func (b *helpSectionBuilder) appendSection(label string) error {
	if len(b.sections) >= helpCardMaxSections {
		return fmt.Errorf("help section count exceeds %d", helpCardMaxSections)
	}
	label = renderableHelpText(b.faces.section, label)
	if label == "" {
		return fmt.Errorf("help section cannot be rendered")
	}
	b.sections = append(b.sections, helpCardSection{label: label, height: helpCardSectionHeight})
	return nil
}

func (b *helpSectionBuilder) appendRow(line string) error {
	if len(b.sections) == 0 {
		b.sections = append(b.sections, helpCardSection{label: "명령어", height: helpCardSectionHeight})
	}
	if b.rowCount >= helpCardMaxRows {
		return fmt.Errorf("help row count exceeds %d", helpCardMaxRows)
	}
	row, err := layoutHelpRow(line, b.faces)
	if err != nil {
		return err
	}
	last := len(b.sections) - 1
	b.sections[last].rows = append(b.sections[last].rows, row)
	b.sections[last].height += row.height
	b.rowCount++
	return nil
}

func (b *helpSectionBuilder) build() ([]helpCardSection, error) {
	if len(b.sections) == 0 || b.rowCount == 0 {
		return nil, fmt.Errorf("help card has no renderable commands")
	}
	for _, section := range b.sections {
		if len(section.rows) == 0 {
			return nil, fmt.Errorf("help section %q has no commands", section.label)
		}
	}
	return b.sections, nil
}

func helpSectionLabel(line string) (string, bool) {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return strings.TrimSpace(line[1 : len(line)-1]), true
	}
	return "", false
}

func layoutHelpRow(line string, faces *helpCardFonts) (helpCardRow, error) {
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

func splitHelpColumns(line string) (command, description string) {
	if head, rest, ok := strings.Cut(line, " - "); ok {
		return strings.TrimSpace(head), strings.TrimSpace(rest)
	}
	if label, rest, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(strings.TrimSpace(label), "!") {
		return strings.TrimSpace(label), strings.TrimSpace(rest)
	}
	return strings.TrimSpace(line), ""
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
	lines := make([]string, 0, helpCardMaxWrappedLines)
	for len(runes) > 0 {
		line, rest := nextHelpWrappedLine(face, runes, maxWidth)
		if line != "" {
			lines = append(lines, line)
		}
		runes = rest
	}
	return lines
}

func nextHelpWrappedLine(face font.Face, runes []rune, maxWidth int) (line string, rest []rune) {
	end := fittingHelpPrefix(face, runes, maxWidth)
	if end < len(runes) {
		end = preferredHelpBreak(runes, end)
	}
	line = strings.TrimSpace(string(runes[:end]))
	rest = runes[end:]
	for len(rest) > 0 && rest[0] == ' ' {
		rest = rest[1:]
	}
	return line, rest
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
