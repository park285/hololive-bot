package live

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 읽기 모델은 PendingEnds 존재만으로 현재 미해결 종료를 판정하면 안 된다.
func TestReduceRetainsSupersededEndEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence []Evidence
		status   domain.LiveStatus
	}{
		{name: "repeated terminal end", evidence: []Evidence{liveA(), endA(), endA()}, status: domain.LiveStatusEnded},
		{name: "older end after newer live", evidence: []Evidence{lateLiveA(), endA()}, status: domain.LiveStatusLive},
		{name: "same-time end after live", evidence: []Evidence{sameTimeLiveA(), endA()}, status: domain.LiveStatusLive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mustReduceAll(t, emptyState(), tc.evidence, 0)
			session := sessionOf(got)

			if session.Status != tc.status || session.Clock.EndCandidateKind != nil || len(got.PendingEnds) != 1 {
				t.Fatalf("status=%s candidate=%v pending=%d", session.Status, session.Clock.EndCandidateKind, len(got.PendingEnds))
			}
		})
	}
}
