package ops

import "strings"

type communityShortsMarkdownColumn struct {
	Header     string
	AlignRight bool
}

func writeCommunityShortsMarkdownHeading(builder *strings.Builder, level int, title string) {
	if builder == nil {
		return
	}
	if level <= 0 {
		level = 1
	}
	builder.WriteString(strings.Repeat("#", level))
	builder.WriteString(" ")
	builder.WriteString(strings.TrimSpace(title))
	builder.WriteString("\n\n")
}

func writeCommunityShortsMarkdownKV(builder *strings.Builder, key string, value string) {
	if builder == nil {
		return
	}
	builder.WriteString("- ")
	builder.WriteString(strings.TrimSpace(key))
	builder.WriteString(": ")
	builder.WriteString(value)
	builder.WriteString("\n")
}

func writeCommunityShortsMarkdownTable(
	builder *strings.Builder,
	columns []communityShortsMarkdownColumn,
	rows [][]string,
) {
	if builder == nil || len(columns) == 0 {
		return
	}

	builder.WriteString("\n| ")
	for i := range columns {
		if i > 0 {
			builder.WriteString(" | ")
		}
		builder.WriteString(columns[i].Header)
	}
	builder.WriteString(" |\n| ")
	for i := range columns {
		if i > 0 {
			builder.WriteString(" | ")
		}
		if columns[i].AlignRight {
			builder.WriteString("---:")
			continue
		}
		builder.WriteString("---")
	}
	builder.WriteString(" |\n")

	for rowIndex := range rows {
		row := rows[rowIndex]
		builder.WriteString("| ")
		for columnIndex := range columns {
			if columnIndex > 0 {
				builder.WriteString(" | ")
			}
			if columnIndex < len(row) {
				builder.WriteString(row[columnIndex])
			}
		}
		builder.WriteString(" |\n")
	}
}

func writeCommunityShortsMarkdownMessage(builder *strings.Builder, message string) {
	if builder == nil {
		return
	}
	builder.WriteString(strings.TrimSpace(message))
	builder.WriteString("\n")
}

func writeCommunityShortsMarkdownSectionTableOrMessage(
	builder *strings.Builder,
	level int,
	title string,
	columns []communityShortsMarkdownColumn,
	rows [][]string,
	emptyMessage string,
) {
	writeCommunityShortsMarkdownHeading(builder, level, title)
	if len(rows) == 0 {
		writeCommunityShortsMarkdownMessage(builder, emptyMessage)
		return
	}
	writeCommunityShortsMarkdownTable(builder, columns, rows)
}

func formatCommunityShortsMarkdownCode(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "\\`") + "`"
}

func promoteCommunityShortsMarkdownHeadings(markdown string, depth int) string {
	if depth <= 0 || strings.TrimSpace(markdown) == "" {
		return markdown
	}

	lines := strings.Split(markdown, "\n")
	prefix := strings.Repeat("#", depth)
	for i := range lines {
		trimmed := strings.TrimLeft(lines[i], " ")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}

		heading := trimmed
		count := 0
		for count < len(heading) && heading[count] == '#' {
			count++
		}
		if count == 0 || count >= len(heading) || heading[count] != ' ' {
			continue
		}

		indent := lines[i][:len(lines[i])-len(trimmed)]
		lines[i] = indent + prefix + heading
	}
	return strings.Join(lines, "\n")
}
