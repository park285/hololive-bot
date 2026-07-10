// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	repository *member.Repository,
	cacheClient cache.Client,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := buildMemberCache(ctx, repository, cacheClient, logger)
	if err != nil {
		return nil, err
	}

	if cacheClient == nil {
		logger.Warn("Cache service is nil; member database init skipped")
		return memberCache, nil
	}
	if err := initializeMemberDatabase(ctx, repository, cacheClient, logger); err != nil {
		return nil, err
	}

	return memberCache, nil
}

func initializeMemberDatabase(
	ctx context.Context,
	repository *member.Repository,
	cacheClient cache.Client,
	logger *slog.Logger,
) error {
	members, err := repository.GetAllMembers(ctx)
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

	if err := cacheClient.InitializeMemberDatabase(ctx, memberMap); err != nil {
		return fmt.Errorf("failed to initialize member database: %w", err)
	}
	return nil
}

// ProvideMemberServiceAdapter - 멤버 데이터 제공자 어댑터 생성
func ProvideMemberServiceAdapter(ctx context.Context, memberCache *member.Cache, logger *slog.Logger) member.DataProvider {
	ctx = memberAdapterContext(ctx)

	return member.NewMemberServiceAdapter(ctx, memberCache, logger)
}

func memberAdapterContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

// ProvideProfileService - 프로필 서비스 생성 (번역 사전 로드 포함)
func ProvideProfileService(
	ctx context.Context,
	cacheClient cache.Client,
	members member.DataProvider,
	logger *slog.Logger,
) (*member.ProfileService, error) {
	service, err := member.NewProfileService(cacheClient, members, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile service: %w", err)
	}
	service.PreloadTranslations(ctx)
	return service, nil
}
