package htmlscraper

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
)

func buildOfficialScheduleIdentityIndex(membersData domain.MemberDataProvider) (officialidentity.Index, error) {
	index, err := officialidentity.Build(membersData)
	if err != nil {
		return nil, fmt.Errorf("build official schedule identity index: %w", err)
	}

	return index, nil
}
