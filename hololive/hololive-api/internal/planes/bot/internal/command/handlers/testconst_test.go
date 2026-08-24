package handlers

import "errors"

var (
	errTestStubNoChannel    = errors.New("stub stream provider has no channel data")
	errTestStubNoNextStream = errors.New("stub alarm service has no next stream info")
)

const (
	testRoomID       = "room-1"
	testCalendarRoom = "test-room"
	testHistoryRoom  = "room"

	testMemberAqua   = "Aqua"
	testMemberPekora = "페코라"

	testChannelAqua      = "ch-aqua"
	testChannelMiko      = "ch-miko"
	testYouTubeChannelID = "yt-1"
	testChzzkChannelID   = "cz-1"

	testVideoID         = "AqxEw3kXcgU"
	testContentTypeJPEG = "image/jpeg"
	testTopicMinecraft  = "minecraft"

	testTypeSourceTopic = "topic"
	testTypeSourceTitle = "title"

	testParamAction = "action"
	testParamLimit  = "limit"
	testParamMonth  = "month"
	testParamHours  = "hours"
)
