package domain

import (
	"context"
	"errors"
)

var ErrMemberNotFound = errors.New("member not found")

type MemberDataProvider interface {
	MemberFinder

	GetChannelIDs(ctx context.Context) ([]string, error)
	LoadAllMembers(ctx context.Context) ([]*Member, error)
}

type MemberFinder interface {
	FindMemberByChannelID(ctx context.Context, channelID string) (*Member, error)
	FindMemberByName(ctx context.Context, name string) (*Member, error)
	FindMemberByAlias(ctx context.Context, alias string) (*Member, error)
}
