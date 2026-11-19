package constants

import "time"

var CacheTTL = struct {
	LiveStreams     time.Duration
	UpcomingStreams time.Duration
	ChannelSchedule time.Duration
}{
	LiveStreams:     5 * time.Minute,
	UpcomingStreams: 5 * time.Minute,
	ChannelSchedule: 5 * time.Minute,
}

var ValkeyConfig = struct {
	ReadyTimeout      time.Duration
	BlockingPoolSize  int
	PipelineMultiplex int
}{
	ReadyTimeout:      5 * time.Second,
	BlockingPoolSize:  100,
	PipelineMultiplex: 4,
}
