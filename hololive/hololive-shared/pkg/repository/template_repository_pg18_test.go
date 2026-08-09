package repository_test

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateRepository_UpsertWithPreviousBody(t *testing.T) {
	repo := newTemplateRepository(t)
	ctx := context.Background()
	key := domain.TemplateKey("PG18_OLD_NEW_TEST")

	created, previousBody, err := repo.UpsertWithPreviousBody(ctx, key, nil, "v1")
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Nil(t, previousBody)
	assert.Equal(t, "v1", created.Body)

	updated, previousBody, err := repo.UpsertWithPreviousBody(ctx, key, nil, "v2")
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, previousBody)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "v1", *previousBody)
	assert.Equal(t, "v2", updated.Body)
}
