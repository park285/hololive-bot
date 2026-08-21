// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package repository_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTemplateRepository(t *testing.T) *repository.TemplateRepository {
	t.Helper()
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return repository.NewTemplateRepository(pool, logger)
}

func TestTemplateRepository_List(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()

	t.Run("empty when key filter has no rows", func(t *testing.T) {
		key := domain.TemplateKey("NOT_A_TEMPLATE")
		templates, err := repo.List(ctx, &key, nil)
		require.NoError(t, err)
		assert.Empty(t, templates)
	})

	t.Run("with data and filters", func(t *testing.T) {
		_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "default body")
		require.NoError(t, err)
		channelID := "room_123"
		_, err = repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override body")
		require.NoError(t, err)

		templates, err := repo.List(ctx, nil, nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(templates), 2)

		key := domain.TemplateKeyOutboxShorts
		templates, err = repo.List(ctx, &key, nil)
		require.NoError(t, err)
		assert.Len(t, templates, 2)

		templates, err = repo.List(ctx, &key, &channelID)
		require.NoError(t, err)
		assert.Len(t, templates, 1)
		assert.Equal(t, "room_123", *templates[0].ChannelID)
	})
}

func TestTemplateRepository_Upsert(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()

	t.Run("insert default", func(t *testing.T) {
		tmpl, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "new body")
		require.NoError(t, err)
		assert.NotZero(t, tmpl.ID)
		assert.Equal(t, domain.TemplateKeyOutboxShorts, tmpl.TemplateKey)
		assert.Nil(t, tmpl.ChannelID)
		assert.Equal(t, "new body", tmpl.Body)
	})

	t.Run("update default", func(t *testing.T) {
		_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "updated body")
		require.NoError(t, err)

		found, err := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
		require.NoError(t, err)
		assert.Equal(t, "updated body", found.Body)
	})

	t.Run("insert override", func(t *testing.T) {
		channelID := "room_abc"
		tmpl, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override body")
		require.NoError(t, err)
		assert.NotZero(t, tmpl.ID)
		assert.Equal(t, "room_abc", *tmpl.ChannelID)
	})

	t.Run("update override", func(t *testing.T) {
		channelID := "room_abc"
		_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override updated")
		require.NoError(t, err)

		found, err := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, &channelID)
		require.NoError(t, err)
		assert.Equal(t, "override updated", found.Body)
	})

	t.Run("recover from duplicate key during create", func(t *testing.T) {
		key := domain.TemplateKeyOutboxCommunity

		var wg sync.WaitGroup
		wg.Add(2)
		results := make(chan error, 2)
		for _, body := range []string{"racing body", "resolved body"} {
			go func() {
				defer wg.Done()
				tmpl, err := repo.Upsert(ctx, key, nil, body)
				if err != nil {
					results <- err
					return
				}
				if tmpl == nil || tmpl.Body != body {
					results <- assert.AnError
					return
				}
				results <- nil
			}()
		}
		wg.Wait()
		close(results)

		for err := range results {
			require.NoError(t, err)
		}

		found, err := repo.FindByKeyAndChannel(ctx, key, nil)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Contains(t, []string{"racing body", "resolved body"}, found.Body)
	})
}

func TestTemplateRepository_DeleteOverride(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()

	channelID := "room_del"
	_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxCommunity, &channelID, "to delete")
	require.NoError(t, err)

	err = repo.DeleteOverride(ctx, domain.TemplateKeyOutboxCommunity, channelID)
	require.NoError(t, err)

	found, err := repo.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxCommunity, &channelID)
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestTemplateRepository_GetByKey(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()

	_, err := repo.Upsert(ctx, domain.TemplateKeyOutboxVideo, nil, "default video")
	require.NoError(t, err)

	ch1 := "room_1"
	_, err = repo.Upsert(ctx, domain.TemplateKeyOutboxVideo, &ch1, "override 1")
	require.NoError(t, err)

	ch2 := "room_2"
	_, err = repo.Upsert(ctx, domain.TemplateKeyOutboxVideo, &ch2, "override 2")
	require.NoError(t, err)

	defaultTmpl, overrides, err := repo.GetByKey(ctx, domain.TemplateKeyOutboxVideo)
	require.NoError(t, err)
	require.NotNil(t, defaultTmpl)
	assert.Equal(t, "default video", defaultTmpl.Body)
	require.Len(t, overrides, 2)
	assert.Equal(t, "room_1", *overrides[0].ChannelID)
	assert.Equal(t, "room_2", *overrides[1].ChannelID)
}

func TestTemplateRepository_Revisions(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()

	tmpl, err := repo.Upsert(ctx, domain.TemplateKeyOutboxMilestone, nil, "v1")
	require.NoError(t, err)

	err = repo.CreateRevision(ctx, tmpl.ID, "v0 old body")
	require.NoError(t, err)
	err = repo.CreateRevision(ctx, tmpl.ID, "v0.5 older body")
	require.NoError(t, err)

	revisions, err := repo.GetRevisions(ctx, tmpl.ID, 5)
	require.NoError(t, err)
	assert.Len(t, revisions, 2)

	rev, err := repo.GetRevisionByID(ctx, revisions[0].ID)
	require.NoError(t, err)
	assert.NotNil(t, rev)
}

func TestTemplateRepository_PruneOldRevisions(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()

	tmpl, err := repo.Upsert(ctx, domain.TemplateKeyCmdHelp, nil, "help")
	require.NoError(t, err)

	for range 10 {
		err = repo.CreateRevision(ctx, tmpl.ID, "revision body")
		require.NoError(t, err)
	}

	err = repo.PruneOldRevisions(ctx, tmpl.ID, 5)
	require.NoError(t, err)

	revisions, err := repo.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)
	assert.Len(t, revisions, 5)
}

func TestTemplateRepository_PruneOldRevisions_KeepZeroNoop(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()

	tmpl, err := repo.Upsert(ctx, domain.TemplateKeyCmdHelp, nil, "help")
	require.NoError(t, err)

	for range 3 {
		err = repo.CreateRevision(ctx, tmpl.ID, "revision body")
		require.NoError(t, err)
	}

	err = repo.PruneOldRevisions(ctx, tmpl.ID, 0)
	require.NoError(t, err)

	revisions, err := repo.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)
	assert.Len(t, revisions, 3)
}

func newTemplateRepositoryWithPool(t *testing.T) (*repository.TemplateRepository, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return repository.NewTemplateRepository(pool, logger), pool
}

func blockRevisionInserts(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION test_block_revision_insert() RETURNS trigger AS $$
BEGIN
	RAISE EXCEPTION 'revision insert blocked by test';
END;
$$ LANGUAGE plpgsql`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `CREATE TRIGGER test_block_revision_insert
		BEFORE INSERT ON notification_template_revisions
		FOR EACH ROW EXECUTE FUNCTION test_block_revision_insert()`)
	require.NoError(t, err)

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TRIGGER IF EXISTS test_block_revision_insert ON notification_template_revisions`); err != nil {
			t.Errorf("drop test trigger: %v", err)
		}
	})
}

func TestTemplateRepository_UpsertWithRevision(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_UPSERT_WITH_REVISION")

	created, previousBody, err := repo.UpsertWithRevision(ctx, key, nil, "v1", 0)
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Nil(t, previousBody)
	assert.Equal(t, "v1", created.Body)

	revisions, err := repo.GetRevisions(ctx, created.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, revisions)

	updated, previousBody, err := repo.UpsertWithRevision(ctx, key, nil, "v2", 0)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, previousBody)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "v1", *previousBody)
	assert.Equal(t, "v2", updated.Body)

	revisions, err = repo.GetRevisions(ctx, updated.ID, 10)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	assert.Equal(t, "v1", revisions[0].Body)
}

func TestTemplateRepository_UpsertWithRevision_IdenticalBodyRecordsNoRevision(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_UPSERT_WITH_REVISION_NOOP")

	created, _, err := repo.UpsertWithRevision(ctx, key, nil, "same", 5)
	require.NoError(t, err)

	for range 3 {
		_, previousBody, saveErr := repo.UpsertWithRevision(ctx, key, nil, "same", 5)
		require.NoError(t, saveErr)
		require.NotNil(t, previousBody)
		assert.Equal(t, "same", *previousBody)
	}

	revisions, err := repo.GetRevisions(ctx, created.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, revisions, "identical re-saves must not push revisions into the retention ring")

	changed, previousBody, err := repo.UpsertWithRevision(ctx, key, nil, "next", 5)
	require.NoError(t, err)
	require.NotNil(t, previousBody)
	assert.Equal(t, "same", *previousBody)

	revisions, err = repo.GetRevisions(ctx, changed.ID, 10)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	assert.Equal(t, "same", revisions[0].Body)
}

func TestTemplateRepository_UpsertWithRevision_Override(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_UPSERT_WITH_REVISION_OVERRIDE")
	channelID := "room_i06"

	created, previousBody, err := repo.UpsertWithRevision(ctx, key, &channelID, "o1", 0)
	require.NoError(t, err)
	require.NotNil(t, created.ChannelID)
	assert.Equal(t, channelID, *created.ChannelID)
	assert.Nil(t, previousBody)

	updated, previousBody, err := repo.UpsertWithRevision(ctx, key, &channelID, "o2", 0)
	require.NoError(t, err)
	require.NotNil(t, previousBody)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "o1", *previousBody)

	revisions, err := repo.GetRevisions(ctx, updated.ID, 10)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	assert.Equal(t, "o1", revisions[0].Body)
}

func TestTemplateRepository_UpsertWithRevision_RollsBackWhenRevisionInsertFails(t *testing.T) {
	repo, pool := newTemplateRepositoryWithPool(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_ROLLBACK")

	seeded, _, err := repo.UpsertWithRevision(ctx, key, nil, "v1", 0)
	require.NoError(t, err)

	blockRevisionInserts(t, pool)

	tmpl, previousBody, err := repo.UpsertWithRevision(ctx, key, nil, "v2", 0)
	require.Error(t, err)
	assert.Nil(t, tmpl)
	assert.Nil(t, previousBody)
	assert.ErrorContains(t, err, "create revision")

	found, err := repo.FindByKeyAndChannel(ctx, key, nil)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "v1", found.Body)

	revisions, err := repo.GetRevisions(ctx, seeded.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, revisions)
}

func TestTemplateRepository_UpsertWithRevision_CanceledContextWritesNothing(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_CANCELED")

	seeded, _, err := repo.UpsertWithRevision(ctx, key, nil, "v1", 0)
	require.NoError(t, err)

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()

	tmpl, previousBody, err := repo.UpsertWithRevision(canceledCtx, key, nil, "v2", 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, tmpl)
	assert.Nil(t, previousBody)

	found, err := repo.FindByKeyAndChannel(ctx, key, nil)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "v1", found.Body)

	revisions, err := repo.GetRevisions(ctx, seeded.ID, 10)
	require.NoError(t, err)
	assert.Empty(t, revisions)
}

func TestTemplateRepository_UpsertWithRevision_PrunesInSameTransaction(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_PRUNE")

	seeded, _, err := repo.UpsertWithRevision(ctx, key, nil, "v1", 0)
	require.NoError(t, err)

	for i := range 10 {
		require.NoError(t, repo.CreateRevision(ctx, seeded.ID, fmt.Sprintf("old-%d", i)))
	}

	updated, previousBody, err := repo.UpsertWithRevision(ctx, key, nil, "v2", 3)
	require.NoError(t, err)
	require.NotNil(t, previousBody)
	assert.Equal(t, "v1", *previousBody)

	revisions, err := repo.GetRevisions(ctx, updated.ID, 100)
	require.NoError(t, err)
	require.Len(t, revisions, 3)
	assert.Equal(t, "v1", revisions[0].Body)
}

func TestTemplateRepository_UpsertWithRevision_KeepZeroSkipsPrune(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_PRUNE_ZERO")

	seeded, _, err := repo.UpsertWithRevision(ctx, key, nil, "v1", 0)
	require.NoError(t, err)

	for i := range 3 {
		require.NoError(t, repo.CreateRevision(ctx, seeded.ID, fmt.Sprintf("old-%d", i)))
	}

	updated, _, err := repo.UpsertWithRevision(ctx, key, nil, "v2", 0)
	require.NoError(t, err)

	revisions, err := repo.GetRevisions(ctx, updated.ID, 100)
	require.NoError(t, err)
	assert.Len(t, revisions, 4)
}

// 동시 저장이 서로의 revision 사이에 끼어들면 목록이 더 오래된 본문을 최신으로
// 보여준다. 각 호출이 돌려준 previousBody로 실제 교체 순서를 복원해 GetRevisions
// 순서와 대조한다.
func TestTemplateRepository_UpsertWithRevision_ConcurrentChainOrdering(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("I06_CONCURRENT")

	seeded, _, err := repo.UpsertWithRevision(ctx, key, nil, "b0", 0)
	require.NoError(t, err)

	const writers = 8

	type link struct {
		body     string
		previous string
	}

	var (
		mu      sync.Mutex
		links   = make(map[string]link, writers)
		wg      sync.WaitGroup
		failure = make(chan error, writers)
	)

	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := fmt.Sprintf("w%d", i)
			tmpl, previousBody, err := repo.UpsertWithRevision(ctx, key, nil, body, 0)
			if err != nil {
				failure <- fmt.Errorf("writer %d: %w", i, err)
				return
			}
			if tmpl == nil || previousBody == nil {
				failure <- fmt.Errorf("writer %d: missing previous body", i)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			links[*previousBody] = link{body: body, previous: *previousBody}
		}()
	}
	wg.Wait()
	close(failure)

	for err := range failure {
		require.NoError(t, err)
	}

	require.Len(t, links, writers, "두 writer가 같은 previousBody를 봤다면 갱신이 유실된 것이다")

	var expectedNewestFirst []string
	for current := "b0"; ; {
		next, ok := links[current]
		if !ok {
			break
		}
		expectedNewestFirst = append([]string{next.previous}, expectedNewestFirst...)
		current = next.body
	}
	require.Len(t, expectedNewestFirst, writers)

	revisions, err := repo.GetRevisions(ctx, seeded.ID, 100)
	require.NoError(t, err)
	require.Len(t, revisions, writers)

	actual := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		actual = append(actual, revision.Body)
	}
	assert.Equal(t, expectedNewestFirst, actual)
}
