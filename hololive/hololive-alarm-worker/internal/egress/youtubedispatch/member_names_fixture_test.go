package youtubedispatch

import (
	"context"
	"slices"
	"sync"
)

type staticMemberNames map[string]string

func (n staticMemberNames) GetMemberName(_ context.Context, channelID string) (string, error) {
	return n[channelID], nil
}

type failingMemberNames struct{ err error }

func (n failingMemberNames) GetMemberName(context.Context, string) (string, error) {
	return "", n.err
}

// recordingMemberNames는 표시명을 조회한 채널을 차례로 기록하고 표시명이 없는 것으로 돌려준다.
type recordingMemberNames struct {
	mu       sync.Mutex
	channels []string
}

func (n *recordingMemberNames) GetMemberName(_ context.Context, channelID string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.channels = append(n.channels, channelID)

	return "", nil
}

func (n *recordingMemberNames) lookups() []string {
	n.mu.Lock()
	defer n.mu.Unlock()

	return slices.Clone(n.channels)
}
