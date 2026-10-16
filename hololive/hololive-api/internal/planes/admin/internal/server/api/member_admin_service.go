package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type memberAdminCache interface {
	Refresh(context.Context) error
	InvalidateAliasCache(context.Context, string) error
}

// MemberMutationResult는 정본 저장과 캐시 전파 결과를 구분한다.
type MemberMutationResult struct {
	Persisted   bool `json:"persisted"`
	CacheSynced bool `json:"cacheSynced"`
	MemberID    int  `json:"memberId"`
	Replayed    bool `json:"replayed"`
}

// MemberAdminService는 관리자 멤버 쓰기와 커밋 기록·캐시 동기화 순서를 소유한다.
type MemberAdminService struct {
	repository *member.Repository
	cache      memberAdminCache
	activity   SettingsActivityLogger
	logger     *slog.Logger
}

func NewMemberAdminService(repository *member.Repository, cache memberAdminCache, activity SettingsActivityLogger, logger *slog.Logger) *MemberAdminService {
	return &MemberAdminService{repository: repository, cache: cache, activity: activity, logger: loggerOrDefault(logger)}
}

func (s *MemberAdminService) Create(ctx context.Context, request CreateMemberRequest) (MemberMutationResult, error) {
	m, err := request.member()
	if err != nil {
		return MemberMutationResult{}, err
	}

	return s.create(ctx, m)
}

func (s *MemberAdminService) create(ctx context.Context, m *domain.Member) (MemberMutationResult, error) {
	replayed, err := s.repository.CreateMemberOrMatch(ctx, m)
	if err != nil {
		return MemberMutationResult{}, fmt.Errorf("create admin member: %w", err)
	}

	return s.committed(ctx, m.ID, replayed, "member_add", "Member added", map[string]any{"name": m.Name}, ""), nil
}

func (s *MemberAdminService) SetGraduation(ctx context.Context, id int, graduated bool) (MemberMutationResult, error) {
	if err := s.repository.SetGraduation(ctx, id, graduated); err != nil {
		return MemberMutationResult{}, fmt.Errorf("set member graduation: %w", err)
	}

	return s.committed(ctx, id, false, "member_graduation", "Member graduation status updated", map[string]any{"is_graduated": graduated}, ""), nil
}

func (s *MemberAdminService) UpdateChannelID(ctx context.Context, id int, value string) (MemberMutationResult, error) {
	if err := s.repository.UpdateChannelID(ctx, id, value); err != nil {
		return MemberMutationResult{}, fmt.Errorf("update member channel: %w", err)
	}

	return s.committed(ctx, id, false, "member_channel_update", "Member channel ID updated", map[string]any{"channel_id": value}, ""), nil
}

func (s *MemberAdminService) UpdateMemberName(ctx context.Context, id int, value string) (MemberMutationResult, error) {
	if err := s.repository.UpdateMemberName(ctx, id, value); err != nil {
		return MemberMutationResult{}, fmt.Errorf("update member name: %w", err)
	}

	return s.committed(ctx, id, false, "member_name_update", "Member name updated", map[string]any{"name": value}, ""), nil
}

func (s *MemberAdminService) AddAlias(ctx context.Context, id int, kind, value string) (MemberMutationResult, error) {
	if err := s.repository.AddAlias(ctx, id, kind, value); err != nil {
		return MemberMutationResult{}, fmt.Errorf("add member alias: %w", err)
	}

	return s.committed(ctx, id, false, "member_alias_add", "Member alias added", map[string]any{"type": kind, "alias": value}, value), nil
}

func (s *MemberAdminService) RemoveAlias(ctx context.Context, id int, kind, value string) (MemberMutationResult, error) {
	if err := s.repository.RemoveAlias(ctx, id, kind, value); err != nil {
		return MemberMutationResult{}, fmt.Errorf("remove member alias: %w", err)
	}

	return s.committed(ctx, id, false, "member_alias_remove", "Member alias removed", map[string]any{"type": kind, "alias": value}, value), nil
}

func (s *MemberAdminService) committed(ctx context.Context, id int, replayed bool, kind, summary string, details map[string]any, alias string) MemberMutationResult {
	result := MemberMutationResult{Persisted: true, MemberID: id, Replayed: replayed}
	// 캐시 오류나 요청 취소가 이미 커밋된 변경의 활동 기록을 건너뛰게 하지 않는다.
	if !replayed && s.activity != nil {
		details["member_id"] = id
		details["persisted"] = true
		s.activity.Log(kind, summary, details)
	}

	var err error

	if alias != "" {
		err = s.cache.InvalidateAliasCache(ctx, alias)
	} else {
		err = s.cache.Refresh(ctx)
	}

	result.CacheSynced = err == nil
	if err != nil {
		s.logger.Warn("Member saved but cache synchronization failed", slog.Int("member_id", id), slog.Any("error", err))
	}

	return result
}
