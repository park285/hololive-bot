package repository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestTemplateRevisionBatchPruneFailureRollsBackBodyAndRevision(t *testing.T) {
	repo, pool := newTemplateRepositoryWithPool(t)
	ctx := t.Context()
	key := domain.TemplateKey("BATCH_PRUNE_ROLLBACK")
	created, err := repo.Upsert(ctx, key, nil, "current")
	require.NoError(t, err)
	require.NoError(t, repo.CreateRevision(ctx, created.ID, "older"))

	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_test_revision_delete() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'revision prune rejected'; END $$;
		CREATE TRIGGER reject_test_revision_delete BEFORE DELETE ON notification_template_revisions
		FOR EACH ROW EXECUTE FUNCTION reject_test_revision_delete()`)
	require.NoError(t, err)

	updated, previous, err := repo.UpsertWithRevision(ctx, key, nil, "replacement", 1)
	require.ErrorContains(t, err, "prune revisions")
	require.Nil(t, updated)
	require.Nil(t, previous)

	stored, found, err := repo.FindByKeyAndChannel(ctx, key, nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "current", stored.Body)

	revisions, err := repo.GetRevisions(ctx, created.ID, 10)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	require.Equal(t, "older", revisions[0].Body)
}

func TestTemplateRevisionBatchPrunesByPersistedOrderIncludingNewRevision(t *testing.T) {
	repo, pool := newTemplateRepositoryWithPool(t)
	ctx := t.Context()
	key := domain.TemplateKey("BATCH_PRUNE_FUTURE")
	created, err := repo.Upsert(ctx, key, nil, "current")
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO notification_template_revisions(template_id,body,created_at)
		VALUES($1,'future revision',clock_timestamp()+interval '1 hour')`, created.ID)
	require.NoError(t, err)

	_, previous, err := repo.UpsertWithRevision(ctx, key, nil, "replacement", 1)
	require.NoError(t, err)
	require.NotNil(t, previous)
	require.Equal(t, "current", *previous)

	revisions, err := repo.GetRevisions(ctx, created.ID, 10)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	require.Equal(t, "future revision", revisions[0].Body)
}
