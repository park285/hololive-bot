package parser

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

func ParseThumbnailSources(sources *gjson.Result) []Thumbnail {
	thumbnails := make([]Thumbnail, 0)

	if !sources.IsArray() {
		return thumbnails
	}

	sources.ForEach(func(_, img gjson.Result) bool {
		thumbnails = append(thumbnails, Thumbnail{
			URL:    img.Get("url").String(),
			Width:  int(img.Get("width").Int()),
			Height: int(img.Get("height").Int()),
		})

		return true
	})

	return thumbnails
}

func ParseShortNumber(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" || text == "No" {
		return 0
	}

	text, multiplier := shortNumberBaseAndMultiplier(text)

	text = strings.ReplaceAll(text, ",", "")

	val, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}

	return int64(val * float64(multiplier))
}

func shortNumberBaseAndMultiplier(text string) (string, int64) {
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"K", 1_000},
		{"M", 1_000_000},
		{"B", 1_000_000_000},
	}
	for _, unit := range units {
		if before, ok := strings.CutSuffix(text, unit.suffix); ok {
			return before, unit.multiplier
		}
	}

	return text, 1
}

func ParseViewCount(text string) int64 {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, " views")
	text = strings.TrimSuffix(text, " view")
	text = strings.TrimSuffix(text, "回視聴")
	text = strings.TrimPrefix(text, "조회수")
	text = strings.TrimSuffix(text, "회")
	text = strings.TrimSpace(text)

	multiplier := float64(1)

	for _, unit := range []struct {
		suffix string
		value  float64
	}{
		{"K", 1_000},
		{"M", 1_000_000},
		{"B", 1_000_000_000},
		{"천", 1_000},
		{"만", 10_000},
		{"万", 10_000},
		{"억", 100_000_000},
		{"조", 1_000_000_000_000},
	} {
		if before, ok := strings.CutSuffix(text, unit.suffix); ok {
			text = before
			multiplier = unit.value

			break
		}
	}

	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(text)

	val, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}

	return int64(val * multiplier)
}

func ParseVideoCount(text string) int64 {
	text = strings.TrimSuffix(text, " videos")
	text = strings.TrimSuffix(text, " video")
	text = strings.ReplaceAll(text, ",", "")

	val, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0
	}

	return val
}
