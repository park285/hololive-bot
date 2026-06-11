package parser

import "github.com/tidwall/gjson"

func testVideoParser(renderer gjson.Result, channelID string) *Video {
	videoID := renderer.Get("videoId").String()
	if videoID == "" {
		return nil
	}
	title := renderer.Get("title.runs.0.text").String()
	if title == "" {
		title = renderer.Get("title.simpleText").String()
	}
	return &Video{
		VideoID:   videoID,
		Title:     title,
		ChannelID: channelID,
	}
}
