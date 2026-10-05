package htmlscraper

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
)

func buildOfficialScheduleIdentityIndex(ctx context.Context, membersData domain.MemberDataProvider) (officialidentity.Index, error) {
	index, err := officialidentity.Build(ctx, membersData)
	if err != nil {
		return nil, fmt.Errorf("build official schedule identity index: %w", err)
	}

	return index, nil
}
