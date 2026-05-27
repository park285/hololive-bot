package model

import "context"

type StreamProvider interface {
	GetLiveStreams(ctx context.Context) ([]*Stream, error)
	GetUpcomingStreams(ctx context.Context, hours int) ([]*Stream, error)
	GetChannelSchedule(ctx context.Context, channelID string, hours int, includeLive bool) ([]*Stream, error)
	GetChannel(ctx context.Context, channelID string) (*Channel, error)
}
