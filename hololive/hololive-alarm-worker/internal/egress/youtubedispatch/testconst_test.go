package youtubedispatch

const (
	testRoomCommunity = "room-community"
	testRoomShorts    = "room-shorts"
	testRoomShort     = "room-short"
	testRoomOld       = "room-old"
	testRoomA         = "room-a"
	testRoomOne       = "room-1"
	testRoom1         = "room1"
	testRoom2         = "room2"

	testChannelCh1    = "UCch1"
	testChannelTarget = "UCtarget"
	testChannelStatus = "UC_status"

	testPostOne  = "post-1"
	testPostTwo  = "post-2"
	testShortOne = "short-1"
	testShortTwo = "short-2"
	testVideoOne = "video-1"

	testMessageHello      = "hello"
	testSendFailedMessage = "send failed"
	testTransportOpPost   = "post"

	testCaseNameCommunity = "community"
	testCaseNameShorts    = "shorts"

	testDedupeKeyShortOne      = "youtube-notification:NEW_SHORT:short-1"
	testDedupeKeyCommunityPost = "youtube-notification:COMMUNITY_POST:post-community"

	testTableOutbox               = "youtube_notification_outbox"
	testTableDelivery             = "youtube_notification_delivery"
	testTableDeliveryTelemetry    = "youtube_notification_delivery_telemetry"
	testTableContentAlarmTracking = "youtube_content_alarm_tracking"

	testPayloadVideoOne  = `{"video_id":"v1","title":"영상1"}`
	testPayloadShortOne  = `{"video_id":"s1","title":"쇼츠1"}`
	testPayloadShortTwo  = `{"video_id":"s2","title":"쇼츠2"}`
	testPayloadMilestone = `{"milestone":"100만"}`
)
