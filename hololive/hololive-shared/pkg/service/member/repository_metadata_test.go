package member

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryQueriesPreserveMemberMetadata(t *testing.T) {
	repository, pool := newPGXRepository(t)
	ctx := t.Context()

	const (
		channelID = "UC-repository-metadata"
		name      = "Repository Metadata"
		photo     = "https://example.com/repository.jpg"
	)

	_, err := pool.Exec(ctx, `INSERT INTO members
        (slug, channel_id, english_name, japanese_name, korean_name, short_korean_name,
         aliases, org, suborg, sync_source, photo)
        VALUES ('repository-metadata', $1, $2, 'メタデータ', '메타데이터', '메타',
                '{"ko":["메타 별칭"],"ja":["メタ別名"]}', 'Hololive', 'ID', 'manual', $3)`,
		channelID, name, photo)
	require.NoError(t, err)

	singleQueries := []struct {
		name      string
		query     func() (*domain.Member, error)
		wantPhoto bool
	}{
		{"FindByChannelID", func() (*domain.Member, error) { return repository.FindByChannelID(ctx, channelID) }, false},
		{"FindByName", func() (*domain.Member, error) { return repository.FindByName(ctx, name) }, false},
		{"FindByAlias", func() (*domain.Member, error) { return repository.FindByAlias(ctx, "메타 별칭") }, false},
		{"GetMemberWithPhotoByChannelID", func() (*domain.Member, error) { return repository.GetMemberWithPhotoByChannelID(ctx, channelID) }, true},
		{"FindByNameAndOrg", func() (*domain.Member, error) { return repository.FindByNameAndOrg(ctx, name, "Hololive") }, false},
	}
	for _, query := range singleQueries {
		t.Run(query.name, func(t *testing.T) {
			got, queryErr := query.query()
			require.NoError(t, queryErr)
			assertMemberRepositoryMetadata(t, got)

			if query.wantPhoto {
				require.Equal(t, photo, got.Photo)
			}
		})
	}

	t.Run("GetAllMembers", func(t *testing.T) {
		members, queryErr := repository.GetAllMembers(ctx)
		require.NoError(t, queryErr)

		for _, got := range members {
			if got.ChannelID == channelID {
				assertMemberRepositoryMetadata(t, got)
				require.Equal(t, photo, got.Photo)

				return
			}
		}

		t.Fatal("GetAllMembers omitted the metadata fixture")
	})
	t.Run("GetMembersWithPhoto", func(t *testing.T) {
		members, queryErr := repository.GetMembersWithPhoto(ctx, []string{channelID})
		require.NoError(t, queryErr)

		got := members[channelID]
		assertMemberRepositoryMetadata(t, got)
		require.Equal(t, photo, got.Photo)
	})
	t.Run("FindAllByName", func(t *testing.T) {
		members, queryErr := repository.FindAllByName(ctx, name)
		require.NoError(t, queryErr)
		require.Len(t, members, 1)
		assertMemberRepositoryMetadata(t, members[0])
	})
}

func assertMemberRepositoryMetadata(t *testing.T, got *domain.Member) {
	t.Helper()
	require.NotNil(t, got)
	require.Equal(t, "Repository Metadata", got.Name)
	require.Equal(t, "メタデータ", got.NameJa)
	require.Equal(t, "메타데이터", got.NameKo)
	require.Equal(t, "메타", got.ShortKoreanName)
	require.Equal(t, "Hololive", got.Org)
	require.Equal(t, "ID", got.Suborg)
	require.Equal(t, "manual", got.SyncSource)
	require.Equal(t, &domain.Aliases{Ko: []string{"메타 별칭"}, Ja: []string{"メタ別名"}}, got.Aliases)
}
