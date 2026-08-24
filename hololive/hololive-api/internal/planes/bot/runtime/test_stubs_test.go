package botruntime

import (
	"context"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubIrisClient struct{}

func (s *stubIrisClient) SendMessage(context.Context, string, string, ...iris.SendOption) error {
	return nil
}

func (s *stubIrisClient) SendMessageAccepted(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubIrisClient) SendImage(context.Context, string, []byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubIrisClient) SendMultipleImages(context.Context, string, [][]byte, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}
func (s *stubIrisClient) Ping(context.Context) bool { return true }
func (s *stubIrisClient) GetConfig(context.Context) (*iris.ConfigResponse, error) {
	return &iris.ConfigResponse{}, nil
}

func (s *stubIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return &iris.ReplyAcceptedResponse{}, nil
}

func (s *stubIrisClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return &iris.ReplyStatusSnapshot{}, nil
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

type testAlarmCRUD struct{}

func (testAlarmCRUD) AddAlarm(context.Context, *domain.AddAlarmRequest) (bool, error) {
	return true, nil
}

func (testAlarmCRUD) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return true, nil
}

func (testAlarmCRUD) GetRoomAlarms(context.Context, string) ([]string, error) {
	return []string{}, nil
}

func (testAlarmCRUD) GetRoomAlarmsWithTypes(context.Context, string) ([]*domain.Alarm, error) {
	return []*domain.Alarm{}, nil
}

func (testAlarmCRUD) ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error) {
	return []domain.AlarmListView{}, nil
}

func (testAlarmCRUD) ClearRoomAlarms(context.Context, string) (int, error) {
	return 0, nil
}

func (testAlarmCRUD) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return &domain.NextStreamInfo{}, nil
}

func (testAlarmCRUD) UpdateAlarmAdvanceMinutes(context.Context, int) []int { return []int{5} }
func (testAlarmCRUD) GetTargetMinutes() []int                              { return []int{5} }
func (testAlarmCRUD) SetRoomName(context.Context, string, string) error    { return nil }
func (testAlarmCRUD) SetUserName(context.Context, string, string) error    { return nil }

func (testAlarmCRUD) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return []*domain.AlarmEntry{}, nil
}

func (testAlarmCRUD) WarmCacheFromDB(context.Context) error { return nil }
