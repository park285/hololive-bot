package botruntime

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
)

type stubIrisClient struct{}

func (s *stubIrisClient) SendMessage(context.Context, string, string, ...iris.SendOption) error {
	return nil
}
func (s *stubIrisClient) SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}
func (s *stubIrisClient) SendImage(context.Context, string, []byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}
func (s *stubIrisClient) SendMultipleImages(context.Context, string, [][]byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}
func (s *stubIrisClient) Ping(context.Context) bool                               { return true }
func (s *stubIrisClient) GetConfig(context.Context) (*iris.ConfigResponse, error) { return nil, nil }
func (s *stubIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, nil
}
func (s *stubIrisClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return nil, nil
}
func (s *stubIrisClient) Decrypt(context.Context, string) (string, error) { return "", nil }

type stubMemberDataProvider struct{}

func (s *stubMemberDataProvider) FindMemberByChannelID(string) *domain.Member { return nil }
func (s *stubMemberDataProvider) FindMemberByName(string) *domain.Member      { return nil }
func (s *stubMemberDataProvider) FindMemberByAlias(string) *domain.Member     { return nil }
func (s *stubMemberDataProvider) GetChannelIDs() []string                     { return nil }
func (s *stubMemberDataProvider) GetAllMembers() []*domain.Member             { return nil }
func (s *stubMemberDataProvider) WithContext(context.Context) domain.MemberDataProvider {
	return s
}
func (s *stubMemberDataProvider) FindMembersByName(string) []*domain.Member  { return nil }
func (s *stubMemberDataProvider) FindMembersByAlias(string) []*domain.Member { return nil }
