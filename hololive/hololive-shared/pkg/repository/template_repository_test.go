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
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *repository.TemplateRepository {
	t.Helper()
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return repository.NewTemplateRepository(pool, logger)
}

func TestTemplateRepository_List(t *testing.T) {
	repository := setupTestDB(t)
	ctx := context.Background()

	t.Run("empty when key filter has no rows", func(t *testing.T) {
		key := domain.TemplateKey("NOT_A_TEMPLATE")
		templates, err := repository.List(ctx, &key, nil)
		require.NoError(t, err)
		assert.Empty(t, templates)
	})

	t.Run("with data and filters", func(t *testing.T) {
		_, err := repository.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "default body")
		require.NoError(t, err)
		channelID := "room_123"
		_, err = repository.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override body")
		require.NoError(t, err)

		templates, err := repository.List(ctx, nil, nil)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(templates), 2)

		key := domain.TemplateKeyOutboxShorts
		templates, err = repository.List(ctx, &key, nil)
		require.NoError(t, err)
		assert.Len(t, templates, 2)

		templates, err = repository.List(ctx, &key, &channelID)
		require.NoError(t, err)
		assert.Len(t, templates, 1)
		assert.Equal(t, "room_123", *templates[0].ChannelID)
	})
}

func TestTemplateRepository_Upsert(t *testing.T) {
	repository := setupTestDB(t)
	ctx := context.Background()

	t.Run("insert default", func(t *testing.T) {
		tmpl, err := repository.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "new body")
		require.NoError(t, err)
		assert.NotZero(t, tmpl.ID)
		assert.Equal(t, domain.TemplateKeyOutboxShorts, tmpl.TemplateKey)
		assert.Nil(t, tmpl.ChannelID)
		assert.Equal(t, "new body", tmpl.Body)
	})

	t.Run("update default", func(t *testing.T) {
		_, err := repository.Upsert(ctx, domain.TemplateKeyOutboxShorts, nil, "updated body")
		require.NoError(t, err)

		found, err := repository.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, nil)
		require.NoError(t, err)
		assert.Equal(t, "updated body", found.Body)
	})

	t.Run("insert override", func(t *testing.T) {
		channelID := "room_abc"
		tmpl, err := repository.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override body")
		require.NoError(t, err)
		assert.NotZero(t, tmpl.ID)
		assert.Equal(t, "room_abc", *tmpl.ChannelID)
	})

	t.Run("update override", func(t *testing.T) {
		channelID := "room_abc"
		_, err := repository.Upsert(ctx, domain.TemplateKeyOutboxShorts, &channelID, "override updated")
		require.NoError(t, err)

		found, err := repository.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxShorts, &channelID)
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
				tmpl, err := repository.Upsert(ctx, key, nil, body)
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

		found, err := repository.FindByKeyAndChannel(ctx, key, nil)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Contains(t, []string{"racing body", "resolved body"}, found.Body)
	})
}

func TestTemplateRepository_DeleteOverride(t *testing.T) {
	repository := setupTestDB(t)
	ctx := context.Background()

	channelID := "room_del"
	_, err := repository.Upsert(ctx, domain.TemplateKeyOutboxCommunity, &channelID, "to delete")
	require.NoError(t, err)

	err = repository.DeleteOverride(ctx, domain.TemplateKeyOutboxCommunity, channelID)
	require.NoError(t, err)

	found, err := repository.FindByKeyAndChannel(ctx, domain.TemplateKeyOutboxCommunity, &channelID)
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestTemplateRepository_GetByKey(t *testing.T) {
	repository := setupTestDB(t)
	ctx := context.Background()

	_, err := repository.Upsert(ctx, domain.TemplateKeyOutboxVideo, nil, "default video")
	require.NoError(t, err)

	ch1 := "room_1"
	_, err = repository.Upsert(ctx, domain.TemplateKeyOutboxVideo, &ch1, "override 1")
	require.NoError(t, err)

	ch2 := "room_2"
	_, err = repository.Upsert(ctx, domain.TemplateKeyOutboxVideo, &ch2, "override 2")
	require.NoError(t, err)

	defaultTmpl, overrides, err := repository.GetByKey(ctx, domain.TemplateKeyOutboxVideo)
	require.NoError(t, err)
	require.NotNil(t, defaultTmpl)
	assert.Equal(t, "default video", defaultTmpl.Body)
	require.Len(t, overrides, 2)
	assert.Equal(t, "room_1", *overrides[0].ChannelID)
	assert.Equal(t, "room_2", *overrides[1].ChannelID)
}

func TestTemplateRepository_Revisions(t *testing.T) {
	repository := setupTestDB(t)
	ctx := context.Background()

	tmpl, err := repository.Upsert(ctx, domain.TemplateKeyOutboxMilestone, nil, "v1")
	require.NoError(t, err)

	err = repository.CreateRevision(ctx, tmpl.ID, "v0 old body")
	require.NoError(t, err)
	err = repository.CreateRevision(ctx, tmpl.ID, "v0.5 older body")
	require.NoError(t, err)

	revisions, err := repository.GetRevisions(ctx, tmpl.ID, 5)
	require.NoError(t, err)
	assert.Len(t, revisions, 2)

	rev, err := repository.GetRevisionByID(ctx, revisions[0].ID)
	require.NoError(t, err)
	assert.NotNil(t, rev)
}

func TestTemplateRepository_PruneOldRevisions(t *testing.T) {
	repository := setupTestDB(t)
	ctx := context.Background()

	tmpl, err := repository.Upsert(ctx, domain.TemplateKeyCmdHelp, nil, "help")
	require.NoError(t, err)

	for range 10 {
		err = repository.CreateRevision(ctx, tmpl.ID, "revision body")
		require.NoError(t, err)
	}

	err = repository.PruneOldRevisions(ctx, tmpl.ID, 5)
	require.NoError(t, err)

	revisions, err := repository.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)
	assert.Len(t, revisions, 5)
}

func TestTemplateRepository_PruneOldRevisions_KeepZeroNoop(t *testing.T) {
	repository := setupTestDB(t)
	ctx := context.Background()

	tmpl, err := repository.Upsert(ctx, domain.TemplateKeyCmdHelp, nil, "help")
	require.NoError(t, err)

	for range 3 {
		err = repository.CreateRevision(ctx, tmpl.ID, "revision body")
		require.NoError(t, err)
	}

	err = repository.PruneOldRevisions(ctx, tmpl.ID, 0)
	require.NoError(t, err)

	revisions, err := repository.GetRevisions(ctx, tmpl.ID, 100)
	require.NoError(t, err)
	assert.Len(t, revisions, 3)
}
