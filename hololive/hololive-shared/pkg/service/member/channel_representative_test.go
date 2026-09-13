package member

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

const sharedReviewChannel = "UC-shared-owner"

func seedSharedReviewMembers(t *testing.T) (*Repository, *domain.Member) {
	t.Helper()

	repo, pool := newPGXRepository(t)

	_, err := pool.Exec(t.Context(), `INSERT INTO members(slug,channel_id,english_name,short_korean_name,org,sync_source,aliases)
 VALUES ('review-owner',$1,'holoAN owner','홀로아나','Hololive','manual','{}'),
 ('review-person',$1,'A Person','개인','Hololive','manual','{"ko":["개인"],"ja":[]}')`, sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	owner, err := repo.FindByChannelID(t.Context(), sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if owner.Name != "holoAN owner" {
		t.Fatalf("SQL representative=%s", owner.Name)
	}

	return repo, owner
}

func TestSharedChannelRepresentativeSurvivesIndividualLookups(t *testing.T) {
	repo, owner := seedSharedReviewMembers(t)
	ctx := t.Context()

	client, verifyWrites := sharedChannelRecordingClient(t)

	cache := newMemberCache(repo, client, slog.New(slog.DiscardHandler), CacheConfig{WarmUpChunkSize: 1, WarmUpMaxGoroutines: 4, ValkeyTTL: time.Minute})

	if err := cache.WarmUpCache(ctx); err != nil {
		t.Fatal(err)
	}

	person, err := cache.FindByAlias(ctx, "개인")
	if err != nil {
		t.Fatal(err)
	}

	if person.Name != "A Person" {
		t.Fatalf("individual lookup=%s", person.Name)
	}

	got, err := cache.GetByChannelID(ctx, sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != owner.ID || got.ShortKoreanName != "홀로아나" {
		t.Fatalf("channel/alarm identity changed: %+v", got)
	}

	verifyWrites(owner.ID)
}

func sharedChannelRecordingClient(t *testing.T) (*cachemocks.Client, func(int)) {
	t.Helper()

	var (
		mu     sync.Mutex
		writes []int
	)

	client := cachemocks.NewLenientClient()
	record := func(key string, value any) {
		if !strings.HasSuffix(key, memberChannelKeyPrefix+sharedReviewChannel) {
			return
		}

		member, ok := value.(*domain.Member)
		if !ok {
			t.Errorf("unexpected cached type %T", value)

			return
		}

		mu.Lock()

		writes = append(writes, member.ID)
		mu.Unlock()
	}

	client.SetFunc = func(_ context.Context, key string, value any, _ time.Duration) error {
		record(key, value)

		return nil
	}
	client.MSetFunc = func(_ context.Context, pairs map[string]any, _ time.Duration) error {
		for key, value := range pairs {
			record(key, value)
		}

		return nil
	}

	return client, func(ownerID int) {
		t.Helper()

		mu.Lock()
		defer mu.Unlock()

		if len(writes) == 0 {
			t.Fatal("channel cache was not populated")
		}

		for _, id := range writes {
			if id != ownerID {
				t.Errorf("distributed channel ID=%d want=%d", id, ownerID)
			}
		}
	}
}

func TestPhotoQueriesUseTheChannelRepresentative(t *testing.T) {
	repo, owner := seedSharedReviewMembers(t)

	photo, err := repo.GetMemberWithPhotoByChannelID(t.Context(), sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if photo.ID != owner.ID {
		t.Fatalf("photo representative=%d want=%d", photo.ID, owner.ID)
	}

	photos, err := repo.GetMembersWithPhoto(t.Context(), []string{sharedReviewChannel})
	if err != nil {
		t.Fatal(err)
	}

	if photos[sharedReviewChannel] == nil || photos[sharedReviewChannel].ID != owner.ID {
		t.Fatal("batch photo representative differs")
	}
}

func TestColdChannelLookupRejectsCachedIndividual(t *testing.T) {
	repo, owner := seedSharedReviewMembers(t)

	person, err := repo.FindByName(t.Context(), "A Person")
	if err != nil {
		t.Fatal(err)
	}

	client := cachemocks.NewLenientClient()

	client.GetFunc = func(_ context.Context, _ string, destination any) error {
		target, ok := destination.(*domain.Member)
		if !ok {
			return errors.New("unexpected destination")
		}

		*target = *person

		return nil
	}

	cache := newMemberCache(repo, client, slog.New(slog.DiscardHandler), CacheConfig{ValkeyTTL: time.Minute})

	got, err := cache.GetByChannelID(t.Context(), sharedReviewChannel)
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != owner.ID {
		t.Fatalf("unverified cached individual accepted: %+v", got)
	}
}
