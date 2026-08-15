package targetprojection

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func DefaultPolicySchedules() map[contract.ObservationKind]Schedule {
	return map[contract.ObservationKind]Schedule{
		contract.KindCommunityPage:  {Priority: 40, PollInterval: 2 * time.Minute, Enabled: true},
		contract.KindVideoList:      {Priority: 50, PollInterval: 5 * time.Minute, Enabled: true},
		contract.KindShortsList:     {Priority: 50, PollInterval: 5 * time.Minute, Enabled: true},
		contract.KindLiveSnapshot:   {Priority: 20, PollInterval: 2 * time.Minute, Enabled: true},
		contract.KindViewerSample:   {Priority: 20, PollInterval: 2 * time.Minute, Enabled: true},
		contract.KindChannelStats:   {Priority: 70, PollInterval: 6 * time.Hour, Enabled: true},
		contract.KindChannelProfile: {Priority: 80, PollInterval: 6 * time.Hour, Enabled: true},
		contract.KindChannelPhoto:   {Priority: 80, PollInterval: 6 * time.Hour, Enabled: true},
		contract.KindSchedule:       {Priority: 30, PollInterval: 5 * time.Minute, Enabled: true},
	}
}
