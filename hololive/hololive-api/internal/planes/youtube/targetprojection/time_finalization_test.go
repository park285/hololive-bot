package targetprojection

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestTimeFinalizationTargetsHaveIndependentInputBudget(t *testing.T) {
	inputs := PolicyInputs{OperationalChannelIDs: []string{testOperationalChannelID}}

	for i := range MaxInputLiveCheckVideoCount {
		inputs.LiveCheckVideos = append(inputs.LiveCheckVideos, LiveCheckVideo{
			VideoID: fmt.Sprintf("live-%d", i), ChannelID: testOperationalChannelID, IsUpcoming: i%2 == 0,
		})
	}

	// 운영 채널 네 종류와 전역 일정 하나를 포함해 전체 상한을 채운다.
	for i := range MaxTargetCount - MaxInputLiveCheckVideoCount - 5 {
		inputs.TimeFinalizationVideos = append(inputs.TimeFinalizationVideos, TimeFinalizationVideo{
			VideoID: fmt.Sprintf("ended-%d", i), ChannelID: "outside-roster", NotBefore: projectionNow,
		})
	}

	targets, reasons, err := BuildPolicyTargets(inputs, DefaultPolicySchedules())
	require.NoError(t, err)
	require.Len(t, targets, MaxTargetCount)

	_, _, _, err = normalize(targets, reasons)
	require.NoError(t, err)

	inputs.TimeFinalizationVideos = append(inputs.TimeFinalizationVideos, TimeFinalizationVideo{
		VideoID: "one-too-many", ChannelID: "outside-roster",
	})
	_, _, err = BuildPolicyTargets(inputs, DefaultPolicySchedules())
	require.ErrorIs(t, err, ErrInvalidProjection)
}

func TestTimeFinalizationDefersToLiveAndUpcoming(t *testing.T) {
	inputs := PolicyInputs{
		OperationalChannelIDs: []string{testOperationalChannelID},
		LiveCheckVideos: []LiveCheckVideo{
			{VideoID: "live", ChannelID: testOperationalChannelID},
			{VideoID: "upcoming", ChannelID: testOperationalChannelID, IsUpcoming: true},
		},
		TimeFinalizationVideos: []TimeFinalizationVideo{
			{VideoID: "live", ChannelID: testOperationalChannelID, NotBefore: projectionNow},
			{VideoID: "upcoming", ChannelID: testOperationalChannelID, NotBefore: projectionNow},
			{VideoID: "ended", ChannelID: "outside-roster", NotBefore: projectionNow},
		},
	}
	targets, reasons, err := BuildPolicyTargets(inputs, DefaultPolicySchedules())
	require.NoError(t, err)

	targets, reasons, hash, err := normalize(targets, reasons)
	require.NoError(t, err)

	for _, target := range targets {
		if target.ObservationKind != contract.KindVideoLiveCheck {
			continue
		}

		require.Equal(t, 2*time.Minute, target.PollInterval)

		switch target.SubjectKey {
		case "live":
			require.EqualValues(t, 20, target.Priority)
			require.True(t, target.NotBefore.IsZero())
		case "upcoming":
			require.EqualValues(t, 19, target.Priority)
			require.True(t, target.NotBefore.IsZero())
		case "ended":
			require.EqualValues(t, 18, target.Priority)
			require.Equal(t, projectionNow, target.NotBefore)
		}
	}

	var archiveReasons []TargetReason

	for _, reason := range reasons {
		if reason.ReasonKind == "archive_time_finalization" {
			archiveReasons = append(archiveReasons, reason)
		}
	}

	require.Equal(t, []TargetReason{{
		SubjectKey: "ended", ObservationKind: contract.KindVideoLiveCheck,
		ReasonKind: "archive_time_finalization", ReasonKey: "outside-roster",
	}}, archiveReasons)

	inputs.TimeFinalizationVideos[2].NotBefore = projectionNow.Add(24 * time.Hour)
	targets, reasons, err = BuildPolicyTargets(inputs, DefaultPolicySchedules())
	require.NoError(t, err)

	_, _, nextHash, err := normalize(targets, reasons)
	require.NoError(t, err)
	require.Equal(t, hash, nextHash)
}
