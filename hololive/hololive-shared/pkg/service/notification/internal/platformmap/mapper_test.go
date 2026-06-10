package platformmap

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
	"github.com/stretchr/testify/require"
)

type stubMemberDataProvider struct {
	members []*domain.Member
}

func (m *stubMemberDataProvider) FindMemberByChannelID(channelID string) *domain.Member {
	for _, member := range m.members {
		if member.ChannelID == channelID {
			return member
		}
	}
	return nil
}

func (m *stubMemberDataProvider) FindMemberByName(_ string) *domain.Member { return nil }

func (m *stubMemberDataProvider) FindMemberByAlias(_ string) *domain.Member { return nil }

func (m *stubMemberDataProvider) GetChannelIDs() []string { return nil }

func (m *stubMemberDataProvider) GetAllMembers() []*domain.Member { return m.members }

func (m *stubMemberDataProvider) WithContext(_ context.Context) domain.MemberDataProvider { return m }

func (m *stubMemberDataProvider) FindMembersByName(_ string) []*domain.Member { return nil }

func (m *stubMemberDataProvider) FindMembersByAlias(_ string) []*domain.Member { return nil }

func newTestMapper(t *testing.T, members ...*domain.Member) (*Mapper, cache.Client) {
	t.Helper()

	cacheClient := sharedtestutil.NewTestCacheService(t, t.Context())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	provider := &stubMemberDataProvider{members: members}
	memberDataFn := func() domain.MemberDataProvider { return provider }
	return NewMapper(cacheClient, memberDataFn, logger), cacheClient
}

func registerChannel(t *testing.T, c cache.Client, channelIDs ...string) {
	t.Helper()
	_, err := c.SAdd(t.Context(), AlarmChannelRegistryKey, channelIDs)
	require.NoError(t, err)
}

func assertTwitchHashes(t *testing.T, c cache.Client, wantLogin, wantChannel map[string]string) {
	t.Helper()

	loginMap, err := c.HGetAll(t.Context(), TwitchLoginMapKey)
	require.NoError(t, err)
	require.Equal(t, wantLogin, loginMap)

	channelMap, err := c.HGetAll(t.Context(), TwitchChannelLoginMapKey)
	require.NoError(t, err)
	require.Equal(t, wantChannel, channelMap)
}

func TestSyncForChannel_FreshTwitchMappingCreation(t *testing.T) {
	t.Parallel()

	mapper, c := newTestMapper(t, &domain.Member{
		ChannelID:    "UC_alpha",
		TwitchUserID: "AlphaLogin",
	})

	registerChannel(t, c, "UC_alpha")
	require.NoError(t, c.Set(t.Context(), TwitchLoginMapEmptyKey, "1", 0))
	require.NoError(t, c.Set(t.Context(), TwitchChannelLoginMapEmptyKey, "1", 0))

	require.NoError(t, mapper.SyncForChannel(t.Context(), "UC_alpha"))

	assertTwitchHashes(t, c,
		map[string]string{"alphalogin": "UC_alpha"},
		map[string]string{"UC_alpha": "alphalogin"},
	)

	for _, key := range []string{TwitchLoginMapEmptyKey, TwitchChannelLoginMapEmptyKey} {
		exists, err := c.Exists(t.Context(), key)
		require.NoError(t, err)
		require.False(t, exists, "empty marker %s should be cleared", key)
	}
}

func TestSyncForChannel_ConflictingLoginKeepsExistingOwner(t *testing.T) {
	t.Parallel()

	mapper, c := newTestMapper(t, &domain.Member{
		ChannelID:    "UC_beta",
		TwitchUserID: "SharedLogin",
	})

	registerChannel(t, c, "UC_beta")
	require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "sharedlogin", "UC_alpha"))
	require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_beta", "sharedlogin"))

	require.NoError(t, mapper.SyncForChannel(t.Context(), "UC_beta"))

	assertTwitchHashes(t, c,
		map[string]string{"sharedlogin": "UC_alpha"},
		map[string]string{},
	)
}

func TestSyncForChannel_ChangedLoginReplacesOwnedMapping(t *testing.T) {
	t.Parallel()

	mapper, c := newTestMapper(t, &domain.Member{
		ChannelID:    "UC_alpha",
		TwitchUserID: "NewLogin",
	})

	registerChannel(t, c, "UC_alpha")
	require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "oldlogin", "UC_alpha"))
	require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_alpha", "oldlogin"))

	require.NoError(t, mapper.SyncForChannel(t.Context(), "UC_alpha"))

	assertTwitchHashes(t, c,
		map[string]string{"newlogin": "UC_alpha"},
		map[string]string{"UC_alpha": "newlogin"},
	)
}

func TestSyncForChannel_UnregisteredRemovesOwnedTwitchMapping(t *testing.T) {
	t.Parallel()

	mapper, c := newTestMapper(t, &domain.Member{
		ChannelID:    "UC_alpha",
		TwitchUserID: "AlphaLogin",
	})

	require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "alphalogin", "UC_alpha"))
	require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_alpha", "alphalogin"))
	require.NoError(t, c.HSet(t.Context(), ChzzkChannelMapKey, "UC_alpha", "chzzk_alpha"))

	require.NoError(t, mapper.SyncForChannel(t.Context(), "UC_alpha"))

	assertTwitchHashes(t, c, map[string]string{}, map[string]string{})

	chzzkMap, err := c.HGetAll(t.Context(), ChzzkChannelMapKey)
	require.NoError(t, err)
	require.Equal(t, map[string]string{}, chzzkMap)
}

func TestSyncForChannel_UnregisteredKeepsTwitchLoginOwnedByOther(t *testing.T) {
	t.Parallel()

	mapper, c := newTestMapper(t, &domain.Member{
		ChannelID:    "UC_alpha",
		TwitchUserID: "AlphaLogin",
	})

	require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "alphalogin", "UC_other"))
	require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_alpha", "alphalogin"))

	require.NoError(t, mapper.SyncForChannel(t.Context(), "UC_alpha"))

	assertTwitchHashes(t, c,
		map[string]string{"alphalogin": "UC_other"},
		map[string]string{},
	)
}

func TestSyncForChannel_BlankChannelIDIsNoOp(t *testing.T) {
	t.Parallel()

	mapper, _ := newTestMapper(t)

	require.NoError(t, mapper.SyncForChannel(t.Context(), "   "))
}

func TestSyncForChannel_RegisteredUnknownChannelClearsMappings(t *testing.T) {
	t.Parallel()

	mapper, c := newTestMapper(t)

	registerChannel(t, c, "UC_ghost")
	require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "ghostlogin", "UC_ghost"))
	require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_ghost", "ghostlogin"))
	require.NoError(t, c.HSet(t.Context(), ChzzkChannelMapKey, "UC_ghost", "chzzk_ghost"))

	require.NoError(t, mapper.SyncForChannel(t.Context(), "UC_ghost"))

	assertTwitchHashes(t, c, map[string]string{}, map[string]string{})

	chzzkMap, err := c.HGetAll(t.Context(), ChzzkChannelMapKey)
	require.NoError(t, err)
	require.Equal(t, map[string]string{}, chzzkMap)
}

func TestSyncForChannel_MissingDependenciesError(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("nil cache", func(t *testing.T) {
		t.Parallel()
		mapper := NewMapper(nil, func() domain.MemberDataProvider {
			return &stubMemberDataProvider{}
		}, logger)
		err := mapper.SyncForChannel(context.Background(), "UC_alpha")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cache service not configured")
	})

	t.Run("nil member data", func(t *testing.T) {
		t.Parallel()
		mapper := NewMapper(cachemocks.NewLenientClient(), nil, logger)
		err := mapper.SyncForChannel(context.Background(), "UC_alpha")
		require.Error(t, err)
		require.Contains(t, err.Error(), "member data provider not configured")
	})
}

func TestReconcileTwitchMappingsForChannel_RemoveStaleOnlyWhenOwned(t *testing.T) {
	t.Parallel()

	t.Run("owned stale login removed when desired empty", func(t *testing.T) {
		t.Parallel()
		mapper, c := newTestMapper(t)
		require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "stale", "UC_alpha"))
		require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_alpha", "stale"))

		require.NoError(t, mapper.reconcileTwitchMappingsForChannel(t.Context(), "UC_alpha", ""))

		assertTwitchHashes(t, c, map[string]string{}, map[string]string{})
	})

	t.Run("foreign stale login kept when desired empty", func(t *testing.T) {
		t.Parallel()
		mapper, c := newTestMapper(t)
		require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "stale", "UC_other"))
		require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_alpha", "stale"))

		require.NoError(t, mapper.reconcileTwitchMappingsForChannel(t.Context(), "UC_alpha", ""))

		assertTwitchHashes(t, c,
			map[string]string{"stale": "UC_other"},
			map[string]string{},
		)
	})
}

func TestRemoveStaleTwitchLoginMappingIfOwned(t *testing.T) {
	t.Parallel()

	t.Run("blank login or channel is no-op", func(t *testing.T) {
		t.Parallel()
		mapper, c := newTestMapper(t)
		require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "x", "UC_alpha"))

		require.NoError(t, mapper.removeStaleTwitchLoginMappingIfOwned(t.Context(), "", "UC_alpha"))
		require.NoError(t, mapper.removeStaleTwitchLoginMappingIfOwned(t.Context(), "x", "  "))

		loginMap, err := c.HGetAll(t.Context(), TwitchLoginMapKey)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"x": "UC_alpha"}, loginMap)
	})

	t.Run("owner mismatch keeps mapping", func(t *testing.T) {
		t.Parallel()
		mapper, c := newTestMapper(t)
		require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "login", "UC_owner"))

		require.NoError(t, mapper.removeStaleTwitchLoginMappingIfOwned(t.Context(), "Login", "UC_intruder"))

		loginMap, err := c.HGetAll(t.Context(), TwitchLoginMapKey)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"login": "UC_owner"}, loginMap)
	})

	t.Run("owned mapping removed", func(t *testing.T) {
		t.Parallel()
		mapper, c := newTestMapper(t)
		require.NoError(t, c.HSet(t.Context(), TwitchLoginMapKey, "login", "UC_owner"))

		require.NoError(t, mapper.removeStaleTwitchLoginMappingIfOwned(t.Context(), "login", "UC_owner"))

		loginMap, err := c.HGetAll(t.Context(), TwitchLoginMapKey)
		require.NoError(t, err)
		require.Equal(t, map[string]string{}, loginMap)
	})

	t.Run("empty owner removed", func(t *testing.T) {
		t.Parallel()
		mapper, c := newTestMapper(t)

		require.NoError(t, mapper.removeStaleTwitchLoginMappingIfOwned(t.Context(), "login", "UC_owner"))

		loginMap, err := c.HGetAll(t.Context(), TwitchLoginMapKey)
		require.NoError(t, err)
		require.Equal(t, map[string]string{}, loginMap)
	})
}

func TestClearConflictingTwitchChannelLoginMapping(t *testing.T) {
	t.Parallel()

	mapper, c := newTestMapper(t)
	require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_loser", "sharedlogin"))
	require.NoError(t, c.HSet(t.Context(), TwitchChannelLoginMapKey, "UC_keep", "other"))

	require.NoError(t, mapper.clearConflictingTwitchChannelLoginMapping(t.Context(), "sharedlogin", "UC_winner", "UC_loser"))

	channelMap, err := c.HGetAll(t.Context(), TwitchChannelLoginMapKey)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"UC_keep": "other"}, channelMap)
}

func TestSyncForChannel_PropagatesRegistryError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("registry boom")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mock := cachemocks.NewStrictClient()
	mock.SIsMemberFunc = func(context.Context, string, string) (bool, error) {
		return false, sentinel
	}
	mapper := NewMapper(mock, func() domain.MemberDataProvider {
		return &stubMemberDataProvider{}
	}, logger)

	err := mapper.SyncForChannel(context.Background(), "UC_alpha")
	require.ErrorIs(t, err, sentinel)
	require.Contains(t, err.Error(), "check channel registry membership")
}
