package sourceobservation

import (
	"maps"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func InitialJobContracts() StaticJobContracts {
	jobs := initialSubjectJobContracts()
	maps.Copy(jobs, initialGlobalJobContracts())

	return jobs
}

func initialSubjectJobContracts() StaticJobContracts {
	return StaticJobContracts{
		mustJobID(contract.ProviderYouTubeJS, "community_collect"): mustJobContract(
			mustJobID(contract.ProviderYouTubeJS, "community_collect"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindCommunityPage},
			[]contract.ObservationKind{contract.KindCommunityPage},
			nil,
		),
		mustJobID(contract.ProviderYouTubeJS, "youtubejs_content"): mustJobContract(
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_content"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindVideoList, contract.KindShortsList},
			[]contract.ObservationKind{contract.KindVideoList, contract.KindShortsList},
			nil,
		),
		mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_live"): mustJobContract(
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_live"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindLiveSnapshot},
			[]contract.ObservationKind{contract.KindLiveSnapshot},
			nil,
		),
		// 채널 /live 확인은 방송 탭 snapshot과 lease·재시도 슬롯을 공유하지 않는 별도 exact-subject job이다.
		mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_live_check"): mustJobContract(
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_live_check"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindChannelLiveCheck},
			[]contract.ObservationKind{contract.KindChannelLiveCheck},
			nil,
		),
		mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_metadata"): mustJobContract(
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_channel_metadata"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindChannelProfile, contract.KindChannelPhoto},
			[]contract.ObservationKind{contract.KindChannelProfile, contract.KindChannelPhoto},
			nil,
		),
		mustJobID(contract.ProviderYouTubeJS, "youtubejs_video_live"): mustJobContract(
			mustJobID(contract.ProviderYouTubeJS, "youtubejs_video_live"),
			JobClassSubject, JobMembershipExactSubject, "",
			[]contract.ObservationKind{contract.KindVideoLiveCheck},
			[]contract.ObservationKind{contract.KindVideoLiveCheck},
			nil,
		),
	}
}

func initialGlobalJobContracts() StaticJobContracts {
	return StaticJobContracts{
		mustJobID(contract.ProviderHolodex, "holodex_live"): mustJobContract(
			mustJobID(contract.ProviderHolodex, "holodex_live"),
			JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_live",
			[]contract.ObservationKind{contract.KindLiveSnapshot},
			[]contract.ObservationKind{contract.KindLiveSnapshot},
			[]contract.ObservationKind{contract.KindLiveSnapshot},
		),
		mustJobID(contract.ProviderHolodex, "holodex_metadata"): mustJobContract(
			mustJobID(contract.ProviderHolodex, "holodex_metadata"),
			JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_metadata",
			[]contract.ObservationKind{contract.KindChannelPhoto},
			[]contract.ObservationKind{contract.KindChannelPhoto},
			[]contract.ObservationKind{contract.KindChannelPhoto},
		),
		mustJobID(contract.ProviderHolodex, "holodex_schedule"): mustJobContract(
			mustJobID(contract.ProviderHolodex, "holodex_schedule"),
			JobClassGlobal, JobMembershipCurrentProjection, "global:holodex_schedule",
			[]contract.ObservationKind{contract.KindSchedule},
			[]contract.ObservationKind{contract.KindSchedule},
			[]contract.ObservationKind{contract.KindLiveSnapshot},
		),
		mustJobID(contract.ProviderHololiveOfficial, "official_schedule"): mustJobContract(
			mustJobID(contract.ProviderHololiveOfficial, "official_schedule"),
			JobClassGlobal, JobMembershipExactSubject, "global:hololive-schedule",
			[]contract.ObservationKind{contract.KindSchedule},
			[]contract.ObservationKind{contract.KindSchedule},
			nil,
		),
	}
}

func mustJobID(provider contract.Provider, kind string) JobID {
	return JobID{Provider: provider, Kind: JobKind(kind)}
}

func mustJobContract(
	id JobID,
	class JobClass,
	membership JobMembership,
	leaseSubject string,
	emissions []contract.ObservationKind,
	cadenceKinds []contract.ObservationKind,
	rosterKinds []contract.ObservationKind,
) JobContract {
	job, err := NewJobContract(id, class, membership, leaseSubject, emissions, cadenceKinds, rosterKinds)
	if err != nil {
		panic(err)
	}

	return job
}
