package matcher

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type snapshotLoadProvider struct {
	domain.MemberDataProvider

	load func(context.Context) ([]*domain.Member, error)
}

func (p snapshotLoadProvider) LoadAllMembers(ctx context.Context) ([]*domain.Member, error) {
	return p.load(ctx)
}

func TestSnapshotCancellationIsIsolatedPerCaller(t *testing.T) {
	for _, cancelLeader := range []bool{true, false} {
		t.Run(map[bool]string{true: "leader", false: "follower"}[cancelLeader], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				provider := snapshotLoadProvider{load: func(ctx context.Context) ([]*domain.Member, error) {
					close(entered)

					select {
					case <-release:
						return nil, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}}
				mm := NewMatcher(provider, nil, newMatcherTestLogger())
				leaderCtx, cancelLeaderCtx := context.WithCancel(t.Context())

				defer cancelLeaderCtx()

				followerCtx, cancelFollowerCtx := context.WithCancel(t.Context())

				defer cancelFollowerCtx()

				leader, follower := make(chan error, 1), make(chan error, 1)

				go func() { _, err := mm.getSnapshot(leaderCtx); leader <- err }()

				<-entered

				go func() { _, err := mm.getSnapshot(followerCtx); follower <- err }()

				synctest.Wait()

				canceled, active := follower, leader

				if cancelLeader {
					cancelLeaderCtx()

					canceled, active = leader, follower
				} else {
					cancelFollowerCtx()
				}

				synctest.Wait()

				select {
				case err := <-canceled:
					if !errors.Is(err, context.Canceled) {
						t.Errorf("canceled caller: %v", err)
					}
				default:
					t.Error("canceled caller remained blocked")
				}

				close(release)

				if err := <-active; err != nil {
					t.Fatalf("independent caller: %v", err)
				}
			})
		})
	}
}

func TestSnapshotSharedLoadHasDeadlineAndPreservesPanicsAsErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := snapshotLoadProvider{load: func(ctx context.Context) ([]*domain.Member, error) {
			<-ctx.Done()

			return nil, ctx.Err()
		}}
		mm := NewMatcher(provider, nil, newMatcherTestLogger())

		if _, err := mm.getSnapshot(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unbounded shared load: %v", err)
		}
	})

	provider := snapshotLoadProvider{load: func(context.Context) ([]*domain.Member, error) { panic("load failed") }}
	mm := NewMatcher(provider, nil, newMatcherTestLogger())

	if _, err := mm.getSnapshot(t.Context()); err == nil {
		t.Fatal("panic became successful snapshot")
	}
}

func TestMatchResultsFollowSnapshotGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := newStubMemberProvider([]*domain.Member{{ChannelID: "old-channel", Name: "Existing"}})
		mm := NewMatcher(provider, nil, newMatcherTestLogger())

		if _, err := mm.getSnapshot(t.Context()); err != nil {
			t.Fatal(err)
		}

		time.Sleep(59 * time.Second)

		for _, name := range []string{"Existing", "BrandNew"} {
			if _, _, err := mm.FindBestMatch(t.Context(), name); err != nil {
				t.Fatal(err)
			}
		}

		provider.members = []*domain.Member{{ChannelID: "updated-channel", Name: "Existing"}, {ChannelID: "new-channel", Name: "BrandNew"}}

		time.Sleep(2 * time.Second)

		for _, name := range []string{"Existing", "BrandNew"} {
			candidate, found, err := mm.FindBestMatchWithCandidates(t.Context(), name)
			if err != nil || !found {
				t.Fatalf("fresh candidate %s: %v", name, err)
			}

			actual, found, err := mm.FindBestMatch(t.Context(), name)
			if err != nil || !found || actual.ID != candidate.ID {
				t.Fatalf("stale result for %s: %v, found=%t, err=%v", name, actual, found, err)
			}
		}
	})
}
