package youtubedispatch

import "context"

// staticMemberNames는 테스트용 표시명 원천이다. 등록되지 않은 채널은 표시명이 없는 것으로 돌려준다.
type staticMemberNames map[string]string

func (n staticMemberNames) GetMemberName(_ context.Context, channelID string) (string, error) {
	return n[channelID], nil
}

// failingMemberNames는 표시명 정본 조회가 실패하는 상황을 만든다.
type failingMemberNames struct{ err error }

func (n failingMemberNames) GetMemberName(context.Context, string) (string, error) {
	return "", n.err
}
