package markdown

import "strings"

type Column struct {
	Header     string
	AlignRight bool
}

func WriteHeading(builder *strings.Builder, level int, title string) {
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

func WriteKV(builder *strings.Builder, key string, value string) {
	if builder == nil {
		return
	}
	builder.WriteString("- ")
	builder.WriteString(strings.TrimSpace(key))
	builder.WriteString(": ")
	builder.WriteString(value)
	builder.WriteString("\n")
}

func WriteTable(
	builder *strings.Builder,
	columns []Column,
	rows [][]string,
) {
	if builder == nil || len(columns) == 0 {
		return
	}

	writeTableHeader(builder, columns)
	for rowIndex := range rows {
		writeTableRow(builder, columns, rows[rowIndex])
	}
}

func writeTableHeader(
	builder *strings.Builder,
	columns []Column,
) {
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
}

func writeTableRow(
	builder *strings.Builder,
	columns []Column,
	row []string,
) {
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

func WriteMessage(builder *strings.Builder, message string) {
	if builder == nil {
		return
	}
	builder.WriteString(strings.TrimSpace(message))
	builder.WriteString("\n")
}

func WriteSectionTableOrMessage(
	builder *strings.Builder,
	level int,
	title string,
	columns []Column,
	rows [][]string,
	emptyMessage string,
) {
	WriteHeading(builder, level, title)
	if len(rows) == 0 {
		WriteMessage(builder, emptyMessage)
		return
	}
	WriteTable(builder, columns, rows)
}

func Code(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "\\`") + "`"
}

func PromoteHeadings(markdown string, depth int) string {
	if depth <= 0 || strings.TrimSpace(markdown) == "" {
		return markdown
	}

	lines := strings.Split(markdown, "\n")
	prefix := strings.Repeat("#", depth)
	for i := range lines {
		indent, heading, ok := splitHeading(lines[i])
		if !ok {
			continue
		}

		lines[i] = indent + prefix + heading
	}
	return strings.Join(lines, "\n")
}

func splitHeading(line string) (string, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}

	count := countHeadingMarks(trimmed)
	if count == 0 || count >= len(trimmed) || trimmed[count] != ' ' {
		return "", "", false
	}

	indent := line[:len(line)-len(trimmed)]
	return indent, trimmed, true
}

func countHeadingMarks(heading string) int {
	count := 0
	for count < len(heading) && heading[count] == '#' {
		count++
	}
	return count
}
