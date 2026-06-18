package dispatchoutbox

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// cache.Client(god interface)와 mock이 narrow ClaimKeyReleaser를 그대로 만족하는지
// 컴파일 타임에 고정한다. 이것이 파트1 동등성(소비자는 좁은 메서드 집합만 의존)의 보증이다.
var (
	_ ClaimKeyReleaser = cache.Client(nil)
	_ ClaimKeyReleaser = (*cache.Service)(nil)
)

type fakeClaimKeyReleaser struct {
	calls    int
	lastKeys []string
	ret      int64
	err      error
}

func (f *fakeClaimKeyReleaser) DelMany(_ context.Context, keys []string) (int64, error) {
	f.calls++
	f.lastKeys = append([]string(nil), keys...)
	if f.err != nil {
		return 0, f.err
	}
	if f.ret != 0 {
		return f.ret, nil
	}
	return int64(len(keys)), nil
}

func TestReleaseClaimKeysDeletesPrefixedKeysWhenReleaserSet(t *testing.T) {
	t.Parallel()

	releaser := &fakeClaimKeyReleaser{}
	consumer := NewConsumer(&consumerTestRepository{}, slog.Default(), WithClaimKeyReleaser(releaser))

	err := consumer.ReleaseClaimKeys(context.Background(), []string{
		" notified:claim:room-1:stream-1:100:live ",
		"notified:claim:event:room-1:channel-1:100:fp:live",
	})
	if err != nil {
		t.Fatalf("ReleaseClaimKeys() error = %v", err)
	}
	if releaser.calls != 1 {
		t.Fatalf("DelMany calls = %d, want 1", releaser.calls)
	}
	want := []string{
		"notified:claim:room-1:stream-1:100:live",
		"notified:claim:event:room-1:channel-1:100:fp:live",
	}
	if len(releaser.lastKeys) != len(want) {
		t.Fatalf("DelMany keys = %v, want %v", releaser.lastKeys, want)
	}
	for i := range want {
		if releaser.lastKeys[i] != want[i] {
			t.Fatalf("DelMany keys[%d] = %q, want %q", i, releaser.lastKeys[i], want[i])
		}
	}
}

func TestReleaseClaimKeysSkipsNonPrefixedKeys(t *testing.T) {
	t.Parallel()

	releaser := &fakeClaimKeyReleaser{}
	consumer := NewConsumer(&consumerTestRepository{}, slog.Default(), WithClaimKeyReleaser(releaser))

	err := consumer.ReleaseClaimKeys(context.Background(), []string{
		"alarm:dispatch:claim:room-1:stream-1",
		"invalid:key",
		"",
		"   ",
	})
	if err != nil {
		t.Fatalf("ReleaseClaimKeys() error = %v", err)
	}
	if releaser.calls != 0 {
		t.Fatalf("DelMany calls = %d, want 0 (no prefixed keys)", releaser.calls)
	}
}

func TestReleaseClaimKeysIsNoOpWhenReleaserNil(t *testing.T) {
	t.Parallel()

	consumer := NewConsumer(&consumerTestRepository{}, slog.Default())

	err := consumer.ReleaseClaimKeys(context.Background(), []string{
		"notified:claim:room-1:stream-1:100:live",
	})
	if err != nil {
		t.Fatalf("ReleaseClaimKeys() error = %v, want nil no-op when releaser absent", err)
	}
}

func TestReleaseClaimKeysEmptyInputDoesNotCallReleaser(t *testing.T) {
	t.Parallel()

	releaser := &fakeClaimKeyReleaser{}
	consumer := NewConsumer(&consumerTestRepository{}, slog.Default(), WithClaimKeyReleaser(releaser))

	if err := consumer.ReleaseClaimKeys(context.Background(), nil); err != nil {
		t.Fatalf("ReleaseClaimKeys(nil) error = %v", err)
	}
	if err := consumer.ReleaseClaimKeys(context.Background(), []string{}); err != nil {
		t.Fatalf("ReleaseClaimKeys([]) error = %v", err)
	}
	if releaser.calls != 0 {
		t.Fatalf("DelMany calls = %d, want 0 for empty input", releaser.calls)
	}
}

func TestReleaseClaimKeysWrapsReleaserError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("valkey down")
	releaser := &fakeClaimKeyReleaser{err: sentinel}
	consumer := NewConsumer(&consumerTestRepository{}, slog.Default(), WithClaimKeyReleaser(releaser))

	err := consumer.ReleaseClaimKeys(context.Background(), []string{"notified:claim:room-1:stream-1:100:live"})
	if err == nil {
		t.Fatal("ReleaseClaimKeys() error = nil, want wrapped releaser error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("ReleaseClaimKeys() error = %v, want wrap of %v", err, sentinel)
	}
}
