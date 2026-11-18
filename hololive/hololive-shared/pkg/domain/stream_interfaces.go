package domain

import "context"

type StreamProvider interface {
	GetUpcomingStreams(ctx context.Context, hours int) ([]*Stream, error)
	GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*Stream, error)
}
