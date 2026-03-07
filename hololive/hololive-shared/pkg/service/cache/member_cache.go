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

package cache

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/valkey-io/valkey-go"
)

const memberHashKey = "hololive:members"

// InitializeMemberDatabase: 멤버 데이터베이스를 캐시 스토어에 초기화한다. (기존 데이터 삭제 후 갱신)
func (c *Service) InitializeMemberDatabase(ctx context.Context, memberData map[string]string) error {
	if err := c.client.Do(ctx, c.client.B().Del().Key(memberHashKey).Build()).Error(); err != nil {
		c.logger.Error("Failed to clear member database", slog.Any("error", err))
		return NewCacheError("del failed", "del", memberHashKey, err)
	}

	if len(memberData) == 0 {
		c.logger.Info("Member database cleared (no members provided)")
		return nil
	}

	builder := c.client.B().Hset().Key(memberHashKey).FieldValue()
	for name, channelID := range memberData {
		builder = builder.FieldValue(name, channelID)
	}

	if err := c.client.Do(ctx, builder.Build()).Error(); err != nil {
		c.logger.Error("Failed to initialize member database", slog.Any("error", err))
		return NewCacheError("hset failed", "hset", memberHashKey, err)
	}

	c.logger.Info("Member database initialized",
		slog.Int("members", len(memberData)),
	)
	return nil
}

// GetMemberChannelID: 멤버 이름으로 채널 ID를 조회합니다.
// 역호환을 위해 먼저 name:Hololive 키를 시도하고, 실패 시 레거시 키(name만)를 시도합니다.
func (c *Service) GetMemberChannelID(ctx context.Context, memberName string) (string, error) {
	if memberName == "" {
		return "", nil
	}

	// 1. 먼저 name:Hololive 형식 시도
	keyWithOrg := memberName + ":Hololive"
	resp := c.client.Do(ctx, c.client.B().Hget().Key(memberHashKey).Field(keyWithOrg).Build())
	if !valkey.IsValkeyNil(resp.Error()) && resp.Error() == nil {
		if value, err := resp.ToString(); err == nil && value != "" {
			return value, nil
		}
	}

	// 2. 레거시 키 시도 (name만)
	resp = c.client.Do(ctx, c.client.B().Hget().Key(memberHashKey).Field(memberName).Build())
	if valkey.IsValkeyNil(resp.Error()) {
		return "", nil
	}
	if resp.Error() != nil {
		c.logger.Error("Failed to get member channel ID", slog.String("member", memberName), slog.Any("error", resp.Error()))
		return "", NewCacheError("hget failed", "hget", memberHashKey, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		return "", NewCacheError("hget conversion failed", "hget", memberHashKey, err)
	}

	return value, nil
}

// GetAllMembers: 캐시에 저장된 모든 멤버 정보를 조회합니다.
func (c *Service) GetAllMembers(ctx context.Context) (map[string]string, error) {
	resp := c.client.Do(ctx, c.client.B().Hgetall().Key(memberHashKey).Build())
	if resp.Error() != nil {
		c.logger.Error("Failed to get all members", slog.Any("error", resp.Error()))
		return map[string]string{}, NewCacheError("hgetall failed", "hgetall", memberHashKey, resp.Error())
	}

	values, err := resp.AsStrMap()
	if err != nil {
		return map[string]string{}, NewCacheError("hgetall conversion failed", "hgetall", memberHashKey, err)
	}

	return values, nil
}

// GetMemberChannelIDWithOrg: org가 지정된 경우 name:org 키로 채널 ID를 조회합니다.
func (c *Service) GetMemberChannelIDWithOrg(ctx context.Context, memberName, org string) (string, error) {
	if memberName == "" {
		return "", nil
	}

	key := memberName
	if org != "" {
		key = memberName + ":" + org
	}

	resp := c.client.Do(ctx, c.client.B().Hget().Key(memberHashKey).Field(key).Build())
	if valkey.IsValkeyNil(resp.Error()) {
		return "", nil
	}
	if resp.Error() != nil {
		c.logger.Error("Failed to get member channel ID with org",
			slog.String("member", memberName),
			slog.String("org", org),
			slog.Any("error", resp.Error()))
		return "", NewCacheError("hget failed", "hget", memberHashKey, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		return "", NewCacheError("hget conversion failed", "hget", memberHashKey, err)
	}

	return value, nil
}

// GetMemberChannelIDs: 이름으로 매칭되는 모든 채널 ID를 반환합니다 (동명이인 처리용).
// name:org 형식의 키에서 name 부분이 일치하는 모든 항목을 반환합니다.
func (c *Service) GetMemberChannelIDs(ctx context.Context, memberName string) ([]string, error) {
	if memberName == "" {
		return nil, nil
	}

	allMembers, err := c.GetAllMembers(ctx)
	if err != nil {
		return nil, err
	}

	var channelIDs []string
	for key, channelID := range allMembers {
		name := key
		if idx := strings.LastIndex(key, ":"); idx > 0 {
			name = key[:idx]
		}

		if name == memberName {
			channelIDs = append(channelIDs, channelID)
		}
	}

	return channelIDs, nil
}

// AddMember: 멤버 정보를 캐시에 추가하거나 갱신합니다.
func (c *Service) AddMember(ctx context.Context, memberName, channelID string) error {
	if memberName == "" || channelID == "" {
		return fmt.Errorf("member name and channel ID must be provided")
	}

	if err := c.client.Do(ctx, c.client.B().Hset().Key(memberHashKey).FieldValue().FieldValue(memberName, channelID).Build()).Error(); err != nil {
		c.logger.Error("Failed to add member", slog.String("member", memberName), slog.String("channel_id", channelID), slog.Any("error", err))
		return NewCacheError("hset failed", "hset", memberHashKey, err)
	}
	c.logger.Info("Member added/updated",
		slog.String("member", memberName),
		slog.String("channel_id", channelID),
	)
	return nil
}
