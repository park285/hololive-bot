package platformmap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"

	"github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/stringutil"
)

const (
	ChzzkChannelMapKey            = sharedalarmkeys.ChzzkChannelMapKey
	ChzzkChannelMapEmptyKey       = sharedalarmkeys.ChzzkChannelMapEmptyKey
	TwitchLoginMapKey             = sharedalarmkeys.TwitchLoginMapKey
	TwitchLoginMapEmptyKey        = sharedalarmkeys.TwitchLoginMapEmptyKey
	TwitchChannelLoginMapKey      = sharedalarmkeys.TwitchChannelLoginMapKey
	TwitchChannelLoginMapEmptyKey = sharedalarmkeys.TwitchChannelLoginMapEmptyKey
	AlarmChannelRegistryKey       = sharedalarmkeys.AlarmChannelRegistryKey
)

type MemberDataFunc func() domain.MemberDataProvider

type Mapper struct {
	cache        cache.Client
	memberDataFn MemberDataFunc
	logger       *slog.Logger
	mu           sync.Mutex
}

func NewMapper(cacheClient cache.Client, memberDataFn MemberDataFunc, logger *slog.Logger) *Mapper {
	return &Mapper{
		cache:        cacheClient,
		memberDataFn: memberDataFn,
		logger:       logger,
	}
}

func (m *Mapper) memberData() domain.MemberDataProvider {
	if m.memberDataFn == nil {
		return nil
	}
	return m.memberDataFn()
}

func (m *Mapper) SyncAll(ctx context.Context) error {
	if m.cache == nil {
		return errors.New("cache service not configured")
	}

	memberData := m.memberData()
	if memberData == nil {
		return errors.New("member data provider not configured")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	channelIDs, err := m.cache.SMembers(ctx, AlarmChannelRegistryKey)
	if err != nil {
		return fmt.Errorf("get channel registry: %w", err)
	}

	chzzkMappings, twitchMappings, twitchChannelMappings := m.collectPlatformMappings(memberData, channelIDs)

	if err := m.replaceHashMappingsWithEmptyMarker(ctx, ChzzkChannelMapKey, ChzzkChannelMapEmptyKey, chzzkMappings); err != nil {
		return fmt.Errorf("sync chzzk channel mappings: %w", err)
	}

	if err := m.replaceHashMappingsWithEmptyMarker(ctx, TwitchLoginMapKey, TwitchLoginMapEmptyKey, twitchMappings); err != nil {
		return fmt.Errorf("sync twitch login mappings: %w", err)
	}

	if err := m.replaceHashMappingsWithEmptyMarker(ctx, TwitchChannelLoginMapKey, TwitchChannelLoginMapEmptyKey, twitchChannelMappings); err != nil {
		return fmt.Errorf("sync twitch channel login mappings: %w", err)
	}

	if m.logger != nil {
		m.logger.Info("Platform alarm mappings synchronized",
			slog.Int("subscribed_channels", len(channelIDs)),
			slog.Int("chzzk_mappings", len(chzzkMappings)),
			slog.Int("twitch_mappings", len(twitchMappings)),
		)
	}

	return nil
}

func (m *Mapper) SyncForChannel(ctx context.Context, channelID string) error {
	channelID = stringutil.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}

	if err := m.validateDependencies(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	registered, err := m.cache.SIsMember(ctx, AlarmChannelRegistryKey, channelID)
	if err != nil {
		return fmt.Errorf("check channel registry membership: %w", err)
	}

	if !registered {
		return m.removeStaleMappingsForChannel(ctx, channelID)
	}

	return m.syncRegisteredMappingForChannel(ctx, channelID)
}

func (m *Mapper) collectPlatformMappings(memberData domain.MemberDataProvider, channelIDs []string) (map[string]string, map[string]string, map[string]string) {
	chzzkMappings := make(map[string]string, len(channelIDs))
	twitchMappings := make(map[string]string, len(channelIDs))
	twitchChannelMappings := make(map[string]string, len(channelIDs))

	for _, channelID := range channelIDs {
		m.collectMappingForChannel(memberData, channelID, chzzkMappings, twitchMappings, twitchChannelMappings)
	}

	return chzzkMappings, twitchMappings, twitchChannelMappings
}

func (m *Mapper) collectMappingForChannel(
	memberData domain.MemberDataProvider,
	channelID string,
	chzzkMappings map[string]string,
	twitchMappings map[string]string,
	twitchChannelMappings map[string]string,
) {
	member := memberData.FindMemberByChannelID(channelID)
	if member == nil {
		m.logUnknownChannel(channelID)
		return
	}

	if chzzkChannelID := stringutil.TrimSpace(member.ChzzkChannelID); chzzkChannelID != "" {
		chzzkMappings[channelID] = chzzkChannelID
	}

	twitchLogin := stringutil.Normalize(member.TwitchUserID)
	if twitchLogin == "" {
		return
	}

	if existingChannelID, exists := twitchMappings[twitchLogin]; exists && existingChannelID != channelID {
		m.logDuplicateTwitchMapping(twitchLogin, existingChannelID, channelID)
		return
	}

	twitchMappings[twitchLogin] = channelID
	twitchChannelMappings[channelID] = twitchLogin
}

func (m *Mapper) logUnknownChannel(channelID string) {
	if m.logger == nil {
		return
	}

	m.logger.Warn("Skip platform mapping sync for unknown channel",
		slog.String("channel_id", channelID),
	)
}

func (m *Mapper) logDuplicateTwitchMapping(twitchLogin, keptChannelID, ignoredChannelID string) {
	if m.logger == nil {
		return
	}

	m.logger.Warn("Duplicate Twitch login detected while syncing platform mappings",
		slog.String("twitch_login", twitchLogin),
		slog.String("kept_channel_id", keptChannelID),
		slog.String("ignored_channel_id", ignoredChannelID),
	)
}

func (m *Mapper) validateDependencies() error {
	if m.cache == nil {
		return errors.New("cache service not configured")
	}
	if m.memberData() == nil {
		return errors.New("member data provider not configured")
	}
	return nil
}

func (m *Mapper) removeStaleMappingsForChannel(ctx context.Context, channelID string) error {
	if err := m.removeMappingsForChannel(ctx, channelID); err != nil {
		return fmt.Errorf("remove stale platform mappings: %w", err)
	}
	return nil
}

func (m *Mapper) syncRegisteredMappingForChannel(ctx context.Context, channelID string) error {
	member := m.memberData().FindMemberByChannelID(channelID)
	if member == nil {
		return m.removeUnknownMappingsForChannel(ctx, channelID)
	}

	if err := m.syncChzzkMappingForChannel(ctx, channelID, member.ChzzkChannelID); err != nil {
		return err
	}

	twitchLogin := stringutil.Normalize(member.TwitchUserID)
	if err := m.reconcileTwitchMappingsForChannel(ctx, channelID, twitchLogin); err != nil {
		return fmt.Errorf("reconcile twitch mapping: %w", err)
	}

	return nil
}

func (m *Mapper) removeUnknownMappingsForChannel(ctx context.Context, channelID string) error {
	logging.Warn(ctx, m.logger, "platform_mapping.unknown_channel", "Skip platform mapping update for unknown channel",
		slog.String("channel_id", channelID),
	)

	if err := m.removeMappingsForChannel(ctx, channelID); err != nil {
		return fmt.Errorf("remove unknown channel platform mappings: %w", err)
	}
	return nil
}

func (m *Mapper) syncChzzkMappingForChannel(ctx context.Context, channelID, rawChzzkChannelID string) error {
	chzzkChannelID := stringutil.TrimSpace(rawChzzkChannelID)
	if chzzkChannelID == "" {
		if err := m.cache.HDel(ctx, ChzzkChannelMapKey, channelID); err != nil {
			return fmt.Errorf("delete missing chzzk mapping: %w", err)
		}
		return nil
	}

	if err := m.cache.HSet(ctx, ChzzkChannelMapKey, channelID, chzzkChannelID); err != nil {
		return fmt.Errorf("upsert chzzk mapping: %w", err)
	}
	if err := m.cache.Del(ctx, ChzzkChannelMapEmptyKey); err != nil {
		return fmt.Errorf("clear chzzk empty marker: %w", err)
	}
	return nil
}

func (m *Mapper) removeMappingsForChannel(ctx context.Context, channelID string) error {
	if err := m.cache.HDel(ctx, ChzzkChannelMapKey, channelID); err != nil {
		return fmt.Errorf("delete chzzk mapping: %w", err)
	}

	if err := m.reconcileTwitchMappingsForChannel(ctx, channelID, ""); err != nil {
		return fmt.Errorf("delete twitch mapping: %w", err)
	}

	return nil
}

func (m *Mapper) removeStaleTwitchLoginMappingIfOwned(ctx context.Context, login, channelID string) error {
	login = stringutil.Normalize(login)
	channelID = stringutil.TrimSpace(channelID)
	if login == "" || channelID == "" {
		return nil
	}

	owner, err := m.cache.HGet(ctx, TwitchLoginMapKey, login)
	if err != nil {
		return fmt.Errorf("get stale twitch login owner: %w", err)
	}
	if owner != "" && owner != channelID {
		return nil
	}

	if err := m.cache.HDel(ctx, TwitchLoginMapKey, login); err != nil {
		return fmt.Errorf("delete stale twitch login mapping: %w", err)
	}

	return nil
}

func (m *Mapper) reconcileTwitchMappingsForChannel(ctx context.Context, channelID, desiredLogin string) error {
	channelID = stringutil.TrimSpace(channelID)
	desiredLogin = stringutil.Normalize(desiredLogin)

	currentLogin, err := m.currentTwitchChannelLogin(ctx, channelID)
	if err != nil {
		return err
	}

	if err := m.removeChangedTwitchLoginMapping(ctx, currentLogin, channelID, desiredLogin); err != nil {
		return err
	}

	if desiredLogin == "" {
		return nil
	}

	return m.reconcileDesiredTwitchLoginMapping(ctx, channelID, desiredLogin)
}

func (m *Mapper) reconcileDesiredTwitchLoginMapping(ctx context.Context, channelID, desiredLogin string) error {
	existingChannelID, err := m.cache.HGet(ctx, TwitchLoginMapKey, desiredLogin)
	if err != nil {
		return fmt.Errorf("get desired twitch login mapping: %w", err)
	}

	if existingChannelID != "" && existingChannelID != channelID {
		return m.clearConflictingTwitchChannelLoginMapping(ctx, desiredLogin, existingChannelID, channelID)
	}

	return m.upsertTwitchMappingsForChannel(ctx, channelID, desiredLogin)
}

func (m *Mapper) currentTwitchChannelLogin(ctx context.Context, channelID string) (string, error) {
	currentLogin, err := m.cache.HGet(ctx, TwitchChannelLoginMapKey, channelID)
	if err != nil {
		return "", fmt.Errorf("get current twitch channel login: %w", err)
	}
	return stringutil.Normalize(currentLogin), nil
}

func (m *Mapper) removeChangedTwitchLoginMapping(
	ctx context.Context,
	currentLogin string,
	channelID string,
	desiredLogin string,
) error {
	if currentLogin != "" && currentLogin != desiredLogin {
		if err := m.removeStaleTwitchLoginMappingIfOwned(ctx, currentLogin, channelID); err != nil {
			return fmt.Errorf("delete stale twitch login mapping: %w", err)
		}
	}
	if desiredLogin == "" {
		return m.deleteTwitchChannelLoginMapping(ctx, channelID)
	}
	return nil
}

func (m *Mapper) deleteTwitchChannelLoginMapping(ctx context.Context, channelID string) error {
	if err := m.cache.HDel(ctx, TwitchChannelLoginMapKey, channelID); err != nil {
		return fmt.Errorf("delete twitch channel login mapping: %w", err)
	}
	return nil
}

func (m *Mapper) clearConflictingTwitchChannelLoginMapping(
	ctx context.Context,
	desiredLogin string,
	existingChannelID string,
	channelID string,
) error {
	if err := m.cache.HDel(ctx, TwitchChannelLoginMapKey, channelID); err != nil {
		return fmt.Errorf("clear conflicting twitch channel login mapping: %w", err)
	}

	logging.Warn(ctx, m.logger, "platform_mapping.duplicate_twitch_login", "Duplicate Twitch login detected while incrementally syncing platform mappings",
		slog.String("twitch_login", desiredLogin),
		slog.String("kept_channel_id", existingChannelID),
		slog.String("ignored_channel_id", channelID),
	)
	return nil
}

func (m *Mapper) upsertTwitchMappingsForChannel(ctx context.Context, channelID, desiredLogin string) error {
	if err := m.cache.HSet(ctx, TwitchLoginMapKey, desiredLogin, channelID); err != nil {
		return fmt.Errorf("upsert twitch mapping: %w", err)
	}
	if err := m.cache.Del(ctx, TwitchLoginMapEmptyKey); err != nil {
		return fmt.Errorf("clear twitch empty marker: %w", err)
	}

	if err := m.cache.HSet(ctx, TwitchChannelLoginMapKey, channelID, desiredLogin); err != nil {
		return fmt.Errorf("upsert twitch channel login mapping: %w", err)
	}
	if err := m.cache.Del(ctx, TwitchChannelLoginMapEmptyKey); err != nil {
		return fmt.Errorf("clear twitch channel empty marker: %w", err)
	}

	return nil
}
