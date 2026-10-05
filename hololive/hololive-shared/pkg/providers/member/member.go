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

// Package member는 runtime이 멤버 저장소와 캐시 연결로 기동 워밍업된 멤버 캐시를 만드는 provider를 담는다.
package member

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	membersvc "github.com/kapu/hololive-shared/pkg/service/member"
)

// ProvideMemberCache는 PG 정본 멤버 캐시를 기동 워밍업과 함께 만든다.
func ProvideMemberCache(
	ctx context.Context,
	repository *membersvc.Repository,
	cacheClient cache.Client,
	logger *slog.Logger,
) (*membersvc.Cache, error) {
	memberCache, err := membersvc.NewMemberCache(ctx, repository, cacheClient, logger, membersvc.CacheConfig{WarmUp: true})
	if err != nil {
		return nil, fmt.Errorf("failed to create member cache: %w", err)
	}

	return memberCache, nil
}
