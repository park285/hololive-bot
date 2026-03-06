package scraper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestCollectVideoRenderers_BoundedScan(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(`{"contents":`)
	for i := 0; i < maxVideoRendererFallbackNodes+32; i++ {
		builder.WriteString(`{"child":`)
	}
	builder.WriteString(`{"videoRenderer":{"videoId":"too-deep","title":{"runs":[{"text":"Too Deep"}]}}}`)
	for i := 0; i < maxVideoRendererFallbackNodes+32; i++ {
		builder.WriteString(`}`)
	}
	builder.WriteString(`}`)

	renderers := collectVideoRenderers(gjson.Parse(builder.String()).Get("contents"), 1)
	assert.Empty(t, renderers)
}
