package markdown

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteCommunityShortsMarkdownHelpers(t *testing.T) {
	t.Parallel()

	var builder strings.Builder
	WriteHeading(&builder, 1, "YouTube Community/Shorts Report")
	WriteKV(&builder, "mode", Code("recent_window"))
	WriteTable(&builder, []Column{
		{Header: "name"},
		{Header: "count", AlignRight: true},
	}, [][]string{
		{Code("row"), "7"},
	})

	require.Equal(t, strings.Join([]string{
		"# YouTube Community/Shorts Report",
		"",
		"- mode: `recent_window`",
		"",
		"| name | count |",
		"| --- | ---: |",
		"| `row` | 7 |",
	}, "\n")+"\n", builder.String())
}

func TestFormatCommunityShortsMarkdownCodeEscapesBackticks(t *testing.T) {
	t.Parallel()

	require.Equal(t, "`value with \\`backtick\\``", Code("value with `backtick`"))
}

func TestPromoteCommunityShortsMarkdownHeadings(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		"# Top",
		"plain text",
		" ## Nested",
		"not-a-heading#",
		"###AlreadyTight",
		"",
	}, "\n")

	require.Equal(t, strings.Join([]string{
		"## Top",
		"plain text",
		" ### Nested",
		"not-a-heading#",
		"###AlreadyTight",
		"",
	}, "\n"), PromoteHeadings(input, 1))
}
