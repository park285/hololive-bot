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

	members, err := repository.GetAllMembers(ctx)
	require.NoError(t, err)

	var snapshot *domain.Member

	for _, member := range members {
		if member.ChannelID == channelID {
			snapshot = member

			break
		}
	}

	require.NotNil(t, snapshot, "GetAllMembers omitted the metadata fixture")
	assertMemberRepositoryMetadata(t, snapshot)
	require.Equal(t, photo, snapshot.Photo)

	// 단건 조회는 snapshot 적재와 같은 열을 읽으므로 snapshot과 같은 멤버를 돌려준다.
	singleQueries := []struct {
		name  string
		query func() (*domain.Member, error)
	}{
		{"FindByChannelID", func() (*domain.Member, error) { return repository.FindByChannelID(ctx, channelID) }},
		{"FindByName", func() (*domain.Member, error) { return repository.FindByName(ctx, name) }},
		{"FindByAlias", func() (*domain.Member, error) { return repository.FindByAlias(ctx, "메타 별칭") }},
	}
	for _, query := range singleQueries {
		t.Run(query.name, func(t *testing.T) {
			got, queryErr := query.query()
			require.NoError(t, queryErr)
			require.Equal(t, snapshot, got)
		})
	}
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
