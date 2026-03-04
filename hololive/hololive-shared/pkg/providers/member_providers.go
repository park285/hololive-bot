package providers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// ProvideMemberRepository - 멤버 저장소 생성
func ProvideMemberRepository(postgres database.Client, logger *slog.Logger) *member.Repository {
	return member.NewMemberRepository(postgres, logger)
}

// ProvideMemberCache - 멤버 캐시 생성 (초기 워밍업 포함)
func ProvideMemberCache(
	ctx context.Context,
	repo *member.Repository,
	cacheSvc cache.Client,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := buildMemberCache(ctx, repo, cacheSvc, logger)
	if err != nil {
		return nil, err
	}

	if cacheSvc == nil {
		logger.Warn("Cache service is nil; member database init skipped")
		return memberCache, nil
	}

	// Valkey member database 초기화 (이름 -> 채널ID 맵)
	members, err := repo.GetAllMembers(ctx)
	if err != nil {
		logger.Warn("Failed to load members for member database init", slog.Any("error", err))
		members = []*domain.Member{}
	}

	memberMap := make(map[string]string, len(members))
	for _, m := range members {
		if m != nil && m.ChannelID != "" {
			// name:org 형식으로 캐시 키 생성 (동명이인 지원)
			memberMap[m.Name+":"+m.GetOrg()] = m.ChannelID
		}
	}

	if err := cacheSvc.InitializeMemberDatabase(ctx, memberMap); err != nil {
		return nil, fmt.Errorf("failed to initialize member database: %w", err)
	}

	return memberCache, nil
}

// ProvideMemberServiceAdapter - 멤버 데이터 제공자 어댑터 생성
func ProvideMemberServiceAdapter(memberCache *member.Cache, logger *slog.Logger) member.DataProvider {
	return member.NewMemberServiceAdapter(memberCache, logger)
}

// ProvideMembersData - 도메인 인터페이스로 바인딩
func ProvideMembersData(adapterSvc member.DataProvider) member.DataProvider {
	return adapterSvc
}

// ProvideProfileService - 프로필 서비스 생성 (번역 사전 로드 포함)
func ProvideProfileService(
	ctx context.Context,
	cacheSvc cache.Client,
	members member.DataProvider,
	logger *slog.Logger,
) (*member.ProfileService, error) {
	svc, err := member.NewProfileService(cacheSvc, members, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile service: %w", err)
	}
	svc.PreloadTranslations(ctx)
	return svc, nil
}
