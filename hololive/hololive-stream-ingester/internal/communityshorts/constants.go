package communityshorts

const (
	LegacyDeliveryPath         = "legacy_alarm_queue"
	NewDeliveryPath            = "youtube_outbox_dispatcher"
	LegacyStatus               = "blocked"
	DeliveryModeNew            = "new_only"
	DeliveryModeOff            = "disabled"
	DeliveryModePending        = "pending_cutover"
	RuntimeOwnerStreamIngester = "stream-ingester"
	RuntimeOwnerYouTubeScraper = "youtube-scraper"
)
