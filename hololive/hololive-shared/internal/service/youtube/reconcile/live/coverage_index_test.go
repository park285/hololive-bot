package live

import (
	"fmt"
	"slices"
	"testing"
)

func TestLiveCoverageIndexParity(t *testing.T) {
	channelSets := [][]string{nil, {}, {""}, {"a"}, {"a", "a", "b", ""}, {" A ", "한글"}}
	statusSets := [][]string{nil, {}, {"LIVE"}, {"LIVE", "LIVE", "UPCOMING"}, {""}}
	for _, channels := range channelSets {
		for _, statuses := range statusSets {
			index := newLiveCoverageIndex(channels, statuses)
			for _, channel := range []string{"", "a", "b", "c", " A ", "한글"} {
				for _, status := range []string{"", "LIVE", "UPCOMING", "ENDED", "live"} {
					want := channel != "" && slices.Contains(channels, channel) &&
						(len(statuses) == 0 || slices.Contains(statuses, status))
					if got := index.covers(channel, status); got != want {
						t.Fatalf("channels=%q statuses=%q query=(%q,%q): got %t want %t", channels, statuses, channel, status, got, want)
					}
				}
			}
		}
	}
}

func TestLiveCoverageIndexOwnsItsSets(t *testing.T) {
	channels := []string{"a"}
	statuses := []string{"LIVE"}
	index := newLiveCoverageIndex(channels, statuses)
	channels[0], statuses[0] = "b", "ENDED"
	if !index.covers("a", "LIVE") || index.covers("b", "ENDED") {
		t.Fatal("index must not retain mutable input slices")
	}
}

func BenchmarkLiveCoverageMembership(b *testing.B) {
	for _, n := range []int{32, 256, 2048} {
		channels := make([]string, n)
		for i := range channels {
			channels[i] = fmt.Sprintf("UC-%06d", i)
		}
		key := channels[n-1]
		index := newLiveCoverageIndex(channels, []string{"LIVE"})
		b.Run(fmt.Sprintf("scan/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if !slices.Contains(channels, key) {
					b.Fatal("missing channel")
				}
			}
		})
		b.Run(fmt.Sprintf("index/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if !index.covers(key, "LIVE") {
					b.Fatal("missing channel")
				}
			}
		})
	}
}

// 인덱스 구성 비용까지 포함한다. 한 번만 조회할 때는 선형 탐색이 유리할 수 있다.
func BenchmarkLiveCoverageSlotBuildAndRead(b *testing.B) {
	channels := make([]string, 256)
	for i := range channels {
		channels[i] = fmt.Sprintf("UC-%06d", i)
	}
	for _, reads := range []int{1, 32, 256} {
		b.Run(fmt.Sprintf("scan/reads-%d", reads), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				for i := range reads {
					if !slices.Contains(channels, channels[(i*31+255)%len(channels)]) {
						b.Fatal("missing channel")
					}
				}
			}
		})
		b.Run(fmt.Sprintf("index/reads-%d", reads), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				index := newLiveCoverageIndex(channels, []string{"LIVE"})
				for i := range reads {
					if !index.covers(channels[(i*31+255)%len(channels)], "LIVE") {
						b.Fatal("missing channel")
					}
				}
			}
		})
	}
}

func TestLiveCoverageMatcherPromotesOnlyAfterReadBudget(t *testing.T) {
	channels := make([]string, 256)
	for i := range channels {
		channels[i] = fmt.Sprintf("UC-%06d", i)
	}
	matcher := newLiveCoverageMatcher(channels, []string{"LIVE"})
	for range liveCoverageLinearReadBudget {
		if !matcher.covers(channels[255], "LIVE") || matcher.index.channels != nil {
			t.Fatal("small workloads must not allocate an index")
		}
	}
	if !matcher.covers(channels[255], "LIVE") || matcher.index.channels == nil {
		t.Fatal("repeated queries must promote to indexed lookup")
	}
	if matcher.channels != nil || matcher.statuses != nil {
		t.Fatal("promoted matcher must release borrowed slice references")
	}
	for _, channel := range []string{"", "missing", channels[0], channels[255]} {
		for _, status := range []string{"LIVE", "UPCOMING", ""} {
			want := channel != "" && slices.Contains(channels, channel) && status == "LIVE"
			if matcher.covers(channel, status) != want {
				t.Fatal("promotion changed matching semantics")
			}
		}
	}
}

func TestLiveCoverageMatcherSmallQueryDoesNotAllocate(t *testing.T) {
	channels := make([]string, 256)
	channels[255] = "last"
	allocations := testing.AllocsPerRun(100, func() {
		matcher := newLiveCoverageMatcher(channels, nil)
		if !matcher.covers("last", "LIVE") {
			panic("missing channel")
		}
	})
	if allocations != 0 {
		t.Fatalf("single-query matcher allocated %.0f times", allocations)
	}
}

func BenchmarkLiveCoverageAdaptiveSlot(b *testing.B) {
	channels := make([]string, 256)
	for i := range channels {
		channels[i] = fmt.Sprintf("UC-%06d", i)
	}
	for _, reads := range []int{1, 32, 256} {
		b.Run(fmt.Sprintf("reads-%d", reads), func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				matcher := newLiveCoverageMatcher(channels, []string{"LIVE"})
				for i := range reads {
					if !matcher.covers(channels[(i*31+255)%len(channels)], "LIVE") {
						b.Fatal("missing channel")
					}
				}
			}
		})
	}
}
