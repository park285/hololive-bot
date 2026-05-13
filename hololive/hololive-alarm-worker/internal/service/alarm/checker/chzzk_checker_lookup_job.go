package checker

import "strings"

type chzzkLookupJob struct {
	youtubeChannelID string
	chzzkChannelID   string
	subscriberRooms  []string
}

func newChzzkLookupJob(youtubeChannelID string, chzzkChannelID string, subscriberMap map[string][]string) (chzzkLookupJob, bool) {
	job := chzzkLookupJob{
		youtubeChannelID: strings.TrimSpace(youtubeChannelID),
		chzzkChannelID:   strings.TrimSpace(chzzkChannelID),
	}
	if job.youtubeChannelID == "" || job.chzzkChannelID == "" {
		return chzzkLookupJob{}, false
	}
	job.subscriberRooms = subscriberMap[job.youtubeChannelID]
	return job, len(job.subscriberRooms) > 0
}
