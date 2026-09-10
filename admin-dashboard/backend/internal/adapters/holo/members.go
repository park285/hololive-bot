package holo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

type upstreamAliases struct {
	KO []*string `json:"ko"`
	JA []*string `json:"ja"`
}

type upstreamMember struct {
	ID          int64            `json:"id"`
	ChannelID   *string          `json:"channelId"`
	Name        *string          `json:"name"`
	Aliases     *upstreamAliases `json:"aliases"`
	NameJA      *string          `json:"nameJa"`
	NameKO      *string          `json:"nameKo"`
	IsGraduated *bool            `json:"isGraduated"`
}

// GetMembers는 고정 upstream에서 멤버를 조회하고 정수 ID를 손실 없는 문자열로 투영합니다.
func (c *Client) GetMembers(ctx context.Context) (MembersResponse, error) {
	return getOwned[MembersResponse](ctx, c, "/api/holo/members", nil)
}

func invalidResponse() error {
	return proxyBadGateway(errInvalidOwnedResponse)
}

func positiveMemberID(id string) (string, error) {
	value, err := strconv.ParseInt(id, 10, 64)
	if err != nil || value <= 0 || strconv.FormatInt(value, 10) != id {
		return "", errors.New("member ID must be a canonical positive int64")
	}

	return strconv.FormatInt(value, 10), nil
}

func memberPath(id, suffix string) (string, error) {
	canonical, err := positiveMemberID(id)
	if err != nil {
		return "", fmt.Errorf("validate member ID: %w", err)
	}

	return "/api/holo/members/" + url.PathEscape(canonical) + suffix, nil
}
