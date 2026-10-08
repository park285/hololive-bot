package targetprojection

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// 추적 기간에 따라 기본 주기의 1·5·15·30배로 늦추고, 경계는 다음 단계에 속한다.
func TestUnresolvablePollIntervalSteps(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		unresolvedFor time.Duration
		want          time.Duration
	}{
		{unresolvedFor: 0, want: 2 * time.Minute},
		{unresolvedFor: 10*time.Minute - time.Second, want: 2 * time.Minute},
		{unresolvedFor: 10 * time.Minute, want: 10 * time.Minute},
		{unresolvedFor: time.Hour - time.Second, want: 10 * time.Minute},
		{unresolvedFor: time.Hour, want: 30 * time.Minute},
		{unresolvedFor: 24*time.Hour - time.Second, want: 30 * time.Minute},
		{unresolvedFor: 24 * time.Hour, want: time.Hour},
		{unresolvedFor: 256 * 24 * time.Hour, want: time.Hour},
	} {
		if got := unresolvablePollInterval(2*time.Minute, tc.unresolvedFor); got != tc.want {
			t.Fatalf("unresolvablePollInterval(2m, %s) = %s, want %s", tc.unresolvedFor, got, tc.want)
		}
	}
}

// 추적 기간은 UPCOMING 영상 확인의 재확인 주기만 늦추고 LIVE·membership·우선순위·NotBefore는 그대로다.
func TestBuildPolicyTargetsSlowsOnlyUnresolvableUpcomingVideoChecks(t *testing.T) {
	t.Parallel()

	notBefore := time.Date(2026, time.October, 8, 9, 0, 0, 0, time.UTC)

	targets, _, err := BuildPolicyTargets(PolicyInputs{
		OperationalChannelIDs: []string{testOperationalChannelID},
		LiveCheckVideos: []LiveCheckVideo{
			{VideoID: "vid-live", ChannelID: testOperationalChannelID, UnresolvableFor: 2 * time.Hour},
			{VideoID: "vid-upcoming-fresh", ChannelID: testOperationalChannelID, IsUpcoming: true, UnresolvableFor: time.Minute},
			{VideoID: "vid-upcoming-old", ChannelID: testOperationalChannelID, IsUpcoming: true, UnresolvableFor: 2 * time.Hour, NotBefore: notBefore},
		},
	}, DefaultPolicySchedules())
	if err != nil {
		t.Fatal(err)
	}

	byVideo := make(map[string]TargetSpec, len(targets))

	for _, target := range targets {
		if target.ObservationKind == contract.KindVideoLiveCheck {
			byVideo[target.SubjectKey] = target
		}
	}

	if got := byVideo["vid-live"]; got.PollInterval != 2*time.Minute || got.Priority != 20 {
		t.Fatalf("live video target = %+v, want default cadence", got)
	}

	if got := byVideo["vid-upcoming-fresh"]; got.PollInterval != 2*time.Minute || got.Priority != 19 {
		t.Fatalf("freshly unresolvable upcoming target = %+v, want default cadence", got)
	}

	if got := byVideo["vid-upcoming-old"]; got.PollInterval != 30*time.Minute || got.Priority != 19 || !got.NotBefore.Equal(notBefore) || !got.Enabled {
		t.Fatalf("long unresolvable upcoming target = %+v, want 30m cadence with priority and not_before kept", got)
	}
}
