package youtubedispatch

import "context"

type staticMemberNames map[string]string

func (n staticMemberNames) GetMemberName(_ context.Context, channelID string) (string, error) {
	return n[channelID], nil
}

type failingMemberNames struct{ err error }

func (n failingMemberNames) GetMemberName(context.Context, string) (string, error) {
	return "", n.err
}
