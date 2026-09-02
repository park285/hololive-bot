package htmlscraper

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
)

type officialScheduleIdentityIndex = officialidentity.Index

func buildOfficialScheduleIdentityIndex(membersData domain.MemberDataProvider) officialidentity.Index {
	return officialidentity.Build(membersData)
}
