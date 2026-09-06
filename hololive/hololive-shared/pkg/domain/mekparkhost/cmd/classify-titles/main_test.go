package main

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyTitles(t *testing.T) {
	t.Parallel()

	input := `[{"video_id":"one","channel_id":"UC3OH5FKQ3qtl4uRme_vZTgA","title":"#玲銘ミラ"},{"video_id":"two","channel_id":"UC_other","title":"#玲銘ミラ"}]`

	var output bytes.Buffer

	require.NoError(t, classifyTitles(strings.NewReader(input), &output))

	var got []classifiedTitle

	require.NoError(t, jsonv2.Unmarshal(output.Bytes(), &got))
	require.Equal(t, []classifiedTitle{
		{VideoID: "one", Unit: "unit-b", Hosts: []string{"reimei-mira"}, Guests: []string{}},
		{VideoID: "two", Unit: "", Hosts: []string{}, Guests: []string{}},
	}, got)
	require.Error(t, classifyTitles(strings.NewReader(`[{`), &output))
}
