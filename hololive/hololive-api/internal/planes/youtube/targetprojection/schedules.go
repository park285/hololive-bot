package targetprojection

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func DefaultPolicySchedules() map[contract.ObservationKind]Schedule {
	// 채널 /live 확인과 영상 상태 확인은 live_snapshot과 같은 2분 cadence·우선순위를 쓴다.
	// 같은 cadence를 쓰지만 각 job의 lease와 완료·재시도 슬롯은 독립이다.
	live := Schedule{Priority: 20, PollInterval: 2 * time.Minute, Enabled: true}

	return map[contract.ObservationKind]Schedule{
		contract.KindCommunityPage:    {Priority: 40, PollInterval: 2 * time.Minute, Enabled: true},
		contract.KindVideoList:        {Priority: 50, PollInterval: 5 * time.Minute, Enabled: true},
		contract.KindShortsList:       {Priority: 50, PollInterval: 5 * time.Minute, Enabled: true},
		contract.KindLiveSnapshot:     live,
		contract.KindChannelLiveCheck: live,
		contract.KindVideoLiveCheck:   live,
		contract.KindChannelStats:     {Priority: 70, PollInterval: 6 * time.Hour, Enabled: true},
		contract.KindChannelProfile:   {Priority: 80, PollInterval: 6 * time.Hour, Enabled: true},
		contract.KindChannelPhoto:     {Priority: 80, PollInterval: 6 * time.Hour, Enabled: true},
		contract.KindSchedule:         {Priority: 30, PollInterval: 5 * time.Minute, Enabled: true},
	}
}
