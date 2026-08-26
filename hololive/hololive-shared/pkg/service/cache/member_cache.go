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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/valkey-io/valkey-go"
)

const memberHashKey = "hololive:members"

func (c *Service) InitializeMemberDatabase(ctx context.Context, memberData map[string]string) error {
	for field := range memberData {
		if !isCanonicalMemberField(field) {
			return errors.New("initialize member database: member field must use name:org format")
		}
	}

	if err := c.client.Do(ctx, c.client.B().Del().Key(memberHashKey).Build()).Error(); err != nil {
		c.logger.Error("Failed to clear member database", slog.Any("error", err))

		return NewCacheError("del", memberHashKey, err)
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

		return NewCacheError("hset", memberHashKey, err)
	}

	c.logger.Info("Member database initialized",
		slog.Int("members", len(memberData)),
	)

	return nil
}

func (c *Service) GetAllMembers(ctx context.Context) (map[string]string, error) {
	resp := c.client.Do(ctx, c.client.B().Hgetall().Key(memberHashKey).Build())
	if resp.Error() != nil {
		c.logger.Error("Failed to get all members", slog.Any("error", resp.Error()))

		return nil, NewCacheError("hgetall", memberHashKey, resp.Error())
	}

	values, err := resp.AsStrMap()
	if err != nil {
		return nil, NewCacheError("hgetall", memberHashKey, err)
	}

	canonical := make(map[string]string, len(values))
	for field, channelID := range values {
		if isCanonicalMemberField(field) {
			canonical[field] = channelID
		}
	}

	return canonical, nil
}

func (c *Service) GetMemberChannelIDWithOrg(ctx context.Context, memberName, org string) (string, error) {
	if memberName == "" || org == "" {
		return "", nil
	}

	key := memberName + ":" + org

	resp := c.client.Do(ctx, c.client.B().Hget().Key(memberHashKey).Field(key).Build())
	if valkey.IsValkeyNil(resp.Error()) {
		return "", nil
	}

	if resp.Error() != nil {
		c.logger.Error("Failed to get member channel ID with org",
			slog.String("member", memberName),
			slog.String("org", org),
			slog.Any("error", resp.Error()))

		return "", NewCacheError("hget", memberHashKey, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		return "", NewCacheError("hget", memberHashKey, err)
	}

	return value, nil
}

// name:org 형식의 키에서 name 부분이 일치하는 모든 항목을 반환합니다.
func (c *Service) GetMemberChannelIDs(ctx context.Context, memberName string) ([]string, error) {
	if memberName == "" {
		return nil, nil
	}

	allMembers, err := c.GetAllMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all members: %w", err)
	}

	var channelIDs []string

	for field, channelID := range allMembers {
		if memberNameFromField(field) == memberName {
			channelIDs = append(channelIDs, channelID)
		}
	}

	return channelIDs, nil
}

func memberNameFromField(field string) string {
	name, _, found := strings.CutLast(field, ":")
	if found && name != "" {
		return name
	}

	return field
}

func isCanonicalMemberField(field string) bool {
	if strings.Count(field, ":") != 1 {
		return false
	}

	name, org, _ := strings.Cut(field, ":")

	return strings.TrimSpace(name) != "" && strings.TrimSpace(org) != ""
}
