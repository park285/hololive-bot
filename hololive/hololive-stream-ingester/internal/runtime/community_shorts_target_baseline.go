package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

const (
	communityShortsLegacyDeliveryPath  = "legacy_alarm_queue"
	communityShortsNewDeliveryPath     = "youtube_outbox_dispatcher"
	communityShortsLegacyStatus        = "blocked"
	communityShortsDeliveryModeNew     = "new_only"
	communityShortsDeliveryModeOff     = "disabled"
	communityShortsDeliveryModePending = "pending_cutover"
)

type CommunityShortsTargetBaseline struct {
	GeneratedAt  time.Time                              `json:"generated_at"`
	Runtime      CommunityShortsTargetBaselineRuntime   `json:"runtime"`
	Sources      CommunityShortsTargetBaselineSources   `json:"sources"`
	PathMappings []CommunityShortsTargetBaselinePath    `json:"path_mappings"`
	Channels     []CommunityShortsTargetBaselineChannel `json:"channels"`
}

type CommunityShortsTargetBaselineRuntime struct {
	FinalDeliveryOwner              string     `json:"final_delivery_owner"`
	CommunityShortsBigBangEnabled   bool       `json:"community_shorts_bigbang_enabled"`
	CommunityShortsBigBangCutoverAt *time.Time `json:"community_shorts_bigbang_cutover_at,omitempty"`
	TargetChannelCount              int        `json:"target_channel_count"`
}

type CommunityShortsTargetBaselineSources struct {
	OperationalChannels string `json:"operational_channels"`
	TypedSubscriberKeys string `json:"typed_subscriber_keys"`
	RoomSubscriptions   string `json:"room_subscriptions"`
}

type CommunityShortsTargetBaselinePath struct {
	AlarmType                domain.AlarmType `json:"alarm_type"`
	LegacyDeliveryPath       string           `json:"legacy_delivery_path"`
	LegacyStatus             string           `json:"legacy_status"`
	LegacyPathActive         bool             `json:"legacy_path_active"`
	NewDeliveryPath          string           `json:"new_delivery_path"`
	NewPathConfigured        bool             `json:"new_path_configured"`
	CutoverPending           bool             `json:"cutover_pending"`
	FinalDeliveryOwner       string           `json:"final_delivery_owner"`
	FinalDeliveryPath        string           `json:"final_delivery_path"`
	SubscriberKeyPrefix      string           `json:"subscriber_key_prefix"`
	ConfiguredChannelCount   int              `json:"configured_channel_count"`
	AlarmEnabledChannelCount int              `json:"alarm_enabled_channel_count"`
	AlarmEnabledRoomCount    int              `json:"alarm_enabled_room_count"`
	ActivationSource         string           `json:"activation_source"`
}

type CommunityShortsTargetBaselineChannel struct {
	OwnerLabel              string                                      `json:"owner_label"`
	ChannelID               string                                      `json:"channel_id"`
	CommunitySubscribersKey string                                      `json:"community_subscribers_key"`
	ShortsSubscribersKey    string                                      `json:"shorts_subscribers_key"`
	Routes                  []CommunityShortsTargetBaselineChannelRoute `json:"routes"`
}

type CommunityShortsTargetBaselineChannelRoute struct {
	AlarmType             domain.AlarmType `json:"alarm_type"`
	SubscriberKey         string           `json:"subscriber_key"`
	AlarmEnabled          bool             `json:"alarm_enabled"`
	SubscriberRoomCount   int              `json:"subscriber_room_count"`
	LegacyPathActive      bool             `json:"legacy_path_active"`
	NewPathConfigured     bool             `json:"new_path_configured"`
	CutoverPending        bool             `json:"cutover_pending"`
	EffectiveDeliveryMode string           `json:"effective_delivery_mode"`
	FinalDeliveryOwner    string           `json:"final_delivery_owner"`
	FinalDeliveryPath     string           `json:"final_delivery_path"`
}

type CommunityShortsOperationalChannel = communityShortsOperationalChannel

type communityShortsAlarmActivationKey struct {
	channelID string
	alarmType domain.AlarmType
}

func CollectCommunityShortsTargetBaseline(ctx context.Context, cfg *config.Config, logger *slog.Logger) (CommunityShortsTargetBaseline, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	memberRepository := sharedproviders.ProvideMemberRepository(databaseResources.Service, logger)
	members, err := memberRepository.GetAllMembers(ctx)
	if err != nil {
		return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: load members: %w", err)
	}

	alarmRepository := sharedalarm.NewRepository(databaseResources.Service, logger)
	alarms, err := alarmRepository.LoadAll(ctx)
	if err != nil {
		return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: load alarms: %w", err)
	}

	channels := buildCommunityShortsOperationalChannelsFromMembers(members)
	return buildCommunityShortsTargetBaseline(channels, alarms, cfg.Ingestion, time.Now().UTC())
}

func BuildCommunityShortsOperationalChannelsFromMembers(members []*domain.Member) []CommunityShortsOperationalChannel {
	return buildCommunityShortsOperationalChannelsFromMembers(members)
}

func BuildCommunityShortsTargetBaseline(
	channels []CommunityShortsOperationalChannel,
	alarms []*domain.Alarm,
	ingestionCfg config.IngestionConfig,
	generatedAt time.Time,
) (CommunityShortsTargetBaseline, error) {
	return buildCommunityShortsTargetBaseline(channels, alarms, ingestionCfg, generatedAt)
}

func buildCommunityShortsTargetBaseline(
	channels []communityShortsOperationalChannel,
	alarms []*domain.Alarm,
	ingestionCfg config.IngestionConfig,
	generatedAt time.Time,
) (CommunityShortsTargetBaseline, error) {
	if err := validateCommunityShortsOperationalTargets(channels); err != nil {
		return CommunityShortsTargetBaseline{}, fmt.Errorf("build community shorts target baseline: %w", err)
	}

	activationIndex := buildCommunityShortsAlarmActivationIndex(alarms)
	finalOwner := resolveCommunityShortsFinalDeliveryOwner(ingestionCfg)
	cutoverPending := communityShortsCutoverPending(ingestionCfg, generatedAt)

	enabledChannels := make([]CommunityShortsTargetBaselineChannel, 0, len(channels))
	for i := range channels {
		if !channels[i].enabled {
			continue
		}
		channelID := strings.TrimSpace(channels[i].channelID)
		if channelID == "" {
			continue
		}
		targetKeys := sharedalarmkeys.BuildChannelContentAlarmTargetKeys(channelID)
		enabledChannels = append(enabledChannels, CommunityShortsTargetBaselineChannel{
			OwnerLabel:              strings.TrimSpace(channels[i].ownerLabel),
			ChannelID:               channelID,
			CommunitySubscribersKey: targetKeys.CommunitySubscribersKey,
			ShortsSubscribersKey:    targetKeys.ShortsSubscribersKey,
			Routes:                  buildCommunityShortsTargetBaselineRoutes(channelID, finalOwner, activationIndex, cutoverPending),
		})
	}

	slices.SortFunc(enabledChannels, func(left, right CommunityShortsTargetBaselineChannel) int {
		if left.ChannelID != right.ChannelID {
			return strings.Compare(left.ChannelID, right.ChannelID)
		}
		return strings.Compare(left.OwnerLabel, right.OwnerLabel)
	})

	cutoverAt := normalizedCommunityShortsCutoverAt(ingestionCfg.CommunityShortsBigBangCutoverAt)

	return CommunityShortsTargetBaseline{
		GeneratedAt: generatedAt.UTC(),
		Runtime: CommunityShortsTargetBaselineRuntime{
			FinalDeliveryOwner:              finalOwner,
			CommunityShortsBigBangEnabled:   ingestionCfg.CommunityShortsBigBangEnabled,
			CommunityShortsBigBangCutoverAt: cutoverAt,
			TargetChannelCount:              len(enabledChannels),
		},
		Sources: CommunityShortsTargetBaselineSources{
			OperationalChannels: "postgres.members -> resolveCommunityShortsOperationalChannels",
			TypedSubscriberKeys: "alarm typed subscriber keys -> BuildChannelContentAlarmTargetKeys",
			RoomSubscriptions:   "postgres.alarms alarm_types -> community/shorts typed room counts",
		},
		PathMappings: buildCommunityShortsTargetBaselinePaths(enabledChannels, finalOwner, cutoverPending),
		Channels:     enabledChannels,
	}, nil
}

func buildCommunityShortsOperationalChannelsFromMembers(members []*domain.Member) []communityShortsOperationalChannel {
	channels := make([]communityShortsOperationalChannel, 0, len(members))
	seenChannelIDs := make(map[string]struct{}, len(members))
	for i := range members {
		member := members[i]
		if member == nil || member.IsGraduated {
			continue
		}
		channelID := strings.TrimSpace(member.ChannelID)
		if channelID != "" {
			if _, exists := seenChannelIDs[channelID]; exists {
				continue
			}
			seenChannelIDs[channelID] = struct{}{}
		}
		channels = append(channels, communityShortsOperationalChannel{
			ownerLabel: communityShortsTargetOwnerLabel(member),
			channelID:  channelID,
			enabled:    channelID != "",
		})
	}
	return channels
}

func buildCommunityShortsTargetBaselinePaths(
	channels []CommunityShortsTargetBaselineChannel,
	finalOwner string,
	cutoverPending bool,
) []CommunityShortsTargetBaselinePath {
	paths := make([]CommunityShortsTargetBaselinePath, 0, len(communityShortsTargetAlarmTypes()))
	for _, alarmType := range communityShortsTargetAlarmTypes() {
		configuredChannelCount := len(channels)
		alarmEnabledChannelCount := 0
		alarmEnabledRoomCount := 0
		pathCutoverPending := false
		for i := range channels {
			route, ok := communityShortsRouteForType(channels[i].Routes, alarmType)
			if !ok || !route.AlarmEnabled {
				continue
			}
			alarmEnabledChannelCount++
			alarmEnabledRoomCount += route.SubscriberRoomCount
			if route.CutoverPending {
				pathCutoverPending = true
			}
		}

		paths = append(paths, CommunityShortsTargetBaselinePath{
			AlarmType:                alarmType,
			LegacyDeliveryPath:       communityShortsLegacyDeliveryPath,
			LegacyStatus:             communityShortsLegacyStatus,
			LegacyPathActive:         false,
			NewDeliveryPath:          communityShortsNewDeliveryPath,
			NewPathConfigured:        true,
			CutoverPending:           cutoverPending && pathCutoverPending,
			FinalDeliveryOwner:       finalOwner,
			FinalDeliveryPath:        communityShortsFinalDeliveryPath(finalOwner),
			SubscriberKeyPrefix:      communityShortsSubscriberKeyPrefix(alarmType),
			ConfiguredChannelCount:   configuredChannelCount,
			AlarmEnabledChannelCount: alarmEnabledChannelCount,
			AlarmEnabledRoomCount:    alarmEnabledRoomCount,
			ActivationSource:         "postgres.alarms alarm_types",
		})
	}
	return paths
}

func buildCommunityShortsTargetBaselineRoutes(
	channelID string,
	finalOwner string,
	activationIndex map[communityShortsAlarmActivationKey]map[string]struct{},
	cutoverPending bool,
) []CommunityShortsTargetBaselineChannelRoute {
	routes := make([]CommunityShortsTargetBaselineChannelRoute, 0, len(communityShortsTargetAlarmTypes()))
	for _, alarmType := range communityShortsTargetAlarmTypes() {
		roomCount := lookupCommunityShortsAlarmRoomCount(activationIndex, channelID, alarmType)
		routeCutoverPending := cutoverPending && roomCount > 0
		routes = append(routes, CommunityShortsTargetBaselineChannelRoute{
			AlarmType:             alarmType,
			SubscriberKey:         sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType),
			AlarmEnabled:          roomCount > 0,
			SubscriberRoomCount:   roomCount,
			LegacyPathActive:      false,
			NewPathConfigured:     true,
			CutoverPending:        routeCutoverPending,
			EffectiveDeliveryMode: communityShortsEffectiveDeliveryMode(roomCount, routeCutoverPending),
			FinalDeliveryOwner:    finalOwner,
			FinalDeliveryPath:     communityShortsFinalDeliveryPath(finalOwner),
		})
	}
	return routes
}

func buildCommunityShortsAlarmActivationIndex(
	alarms []*domain.Alarm,
) map[communityShortsAlarmActivationKey]map[string]struct{} {
	index := make(map[communityShortsAlarmActivationKey]map[string]struct{})
	for _, alarmRecord := range alarms {
		if alarmRecord == nil {
			continue
		}

		roomID := strings.TrimSpace(alarmRecord.RoomID)
		channelID := strings.TrimSpace(alarmRecord.ChannelID)
		if roomID == "" || channelID == "" {
			continue
		}

		for _, alarmType := range normalizedCommunityShortsAlarmTypes(alarmRecord.AlarmTypes) {
			key := communityShortsAlarmActivationKey{channelID: channelID, alarmType: alarmType}
			roomSet := index[key]
			if roomSet == nil {
				roomSet = make(map[string]struct{})
				index[key] = roomSet
			}
			roomSet[alarmRecord.RegistryKey()] = struct{}{}
		}
	}
	return index
}

func normalizedCommunityShortsAlarmTypes(alarmTypes domain.AlarmTypes) []domain.AlarmType {
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	result := make([]domain.AlarmType, 0, len(alarmTypes))
	seen := make(map[domain.AlarmType]struct{}, len(alarmTypes))
	for _, alarmType := range alarmTypes {
		if alarmType != domain.AlarmTypeCommunity && alarmType != domain.AlarmTypeShorts {
			continue
		}
		if _, ok := seen[alarmType]; ok {
			continue
		}
		seen[alarmType] = struct{}{}
		result = append(result, alarmType)
	}
	return result
}

func communityShortsTargetAlarmTypes() []domain.AlarmType {
	return []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts}
}

func communityShortsSubscriberKeyPrefix(alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return sharedalarmkeys.ChannelSubscribersCommunityPrefix
	case domain.AlarmTypeShorts:
		return sharedalarmkeys.ChannelSubscribersShortsPrefix
	default:
		return ""
	}
}

func communityShortsRouteForType(
	routes []CommunityShortsTargetBaselineChannelRoute,
	alarmType domain.AlarmType,
) (CommunityShortsTargetBaselineChannelRoute, bool) {
	for i := range routes {
		if routes[i].AlarmType == alarmType {
			return routes[i], true
		}
	}
	return CommunityShortsTargetBaselineChannelRoute{}, false
}

func CommunityShortsRouteForType(
	routes []CommunityShortsTargetBaselineChannelRoute,
	alarmType domain.AlarmType,
) (CommunityShortsTargetBaselineChannelRoute, bool) {
	return communityShortsRouteForType(routes, alarmType)
}

func lookupCommunityShortsAlarmRoomCount(
	activationIndex map[communityShortsAlarmActivationKey]map[string]struct{},
	channelID string,
	alarmType domain.AlarmType,
) int {
	return len(activationIndex[communityShortsAlarmActivationKey{
		channelID: strings.TrimSpace(channelID),
		alarmType: alarmType,
	}])
}

func communityShortsEffectiveDeliveryMode(roomCount int, cutoverPending bool) string {
	if roomCount == 0 {
		return communityShortsDeliveryModeOff
	}
	if cutoverPending {
		return communityShortsDeliveryModePending
	}
	return communityShortsDeliveryModeNew
}

func communityShortsCutoverPending(ingestionCfg config.IngestionConfig, generatedAt time.Time) bool {
	if !ingestionCfg.CommunityShortsBigBangEnabled {
		return false
	}
	cutoverAt := normalizedCommunityShortsCutoverAt(ingestionCfg.CommunityShortsBigBangCutoverAt)
	if cutoverAt == nil {
		return false
	}
	return generatedAt.UTC().Before(*cutoverAt)
}

func resolveCommunityShortsFinalDeliveryOwner(ingestionCfg config.IngestionConfig) string {
	if ingestionCfg.CommunityShortsBigBangEnabled {
		return youtubeScraperRuntimeName
	}
	return streamIngesterRuntimeName
}

func normalizedCommunityShortsCutoverAt(cutoverAt time.Time) *time.Time {
	if cutoverAt.IsZero() {
		return nil
	}
	normalized := cutoverAt.UTC()
	return &normalized
}

func communityShortsFinalDeliveryPath(finalOwner string) string {
	trimmedOwner := strings.TrimSpace(finalOwner)
	if trimmedOwner == "" {
		return communityShortsNewDeliveryPath
	}
	return trimmedOwner + "." + communityShortsNewDeliveryPath
}
