package communityshorts

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

type TargetBaseline struct {
	GeneratedAt  time.Time               `json:"generated_at"`
	Runtime      TargetBaselineRuntime   `json:"runtime"`
	Sources      TargetBaselineSources   `json:"sources"`
	PathMappings []TargetBaselinePath    `json:"path_mappings"`
	Channels     []TargetBaselineChannel `json:"channels"`
}

type TargetBaselineRuntime struct {
	FinalDeliveryOwner              string     `json:"final_delivery_owner"`
	CommunityShortsBigBangEnabled   bool       `json:"community_shorts_bigbang_enabled"`
	CommunityShortsBigBangCutoverAt *time.Time `json:"community_shorts_bigbang_cutover_at,omitempty"`
	TargetChannelCount              int        `json:"target_channel_count"`
}

type TargetBaselineSources struct {
	OperationalChannels string `json:"operational_channels"`
	TypedSubscriberKeys string `json:"typed_subscriber_keys"`
	RoomSubscriptions   string `json:"room_subscriptions"`
}

type TargetBaselinePath struct {
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

type TargetBaselineChannel struct {
	OwnerLabel              string                       `json:"owner_label"`
	ChannelID               string                       `json:"channel_id"`
	CommunitySubscribersKey string                       `json:"community_subscribers_key"`
	ShortsSubscribersKey    string                       `json:"shorts_subscribers_key"`
	Routes                  []TargetBaselineChannelRoute `json:"routes"`
}

type TargetBaselineChannelRoute struct {
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

type alarmActivationKey struct {
	channelID string
	alarmType domain.AlarmType
}

func CollectTargetBaseline(ctx context.Context, cfg *config.Config, logger *slog.Logger) (TargetBaseline, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return TargetBaseline{}, fmt.Errorf("collect community shorts target baseline: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return TargetBaseline{}, fmt.Errorf("collect community shorts target baseline: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	memberRepository := sharedproviders.ProvideMemberRepository(databaseResources.Service, logger)
	members, err := memberRepository.GetAllMembers(ctx)
	if err != nil {
		return TargetBaseline{}, fmt.Errorf("collect community shorts target baseline: load members: %w", err)
	}

	alarmRepository := sharedalarm.NewRepository(databaseResources.Service, logger)
	alarms, err := alarmRepository.LoadAll(ctx)
	if err != nil {
		return TargetBaseline{}, fmt.Errorf("collect community shorts target baseline: load alarms: %w", err)
	}

	channels := BuildOperationalChannelsFromMembers(members)
	return BuildTargetBaseline(channels, alarms, cfg.Ingestion, time.Now().UTC())
}

func BuildTargetBaseline(
	channels []OperationalChannel,
	alarms []*domain.Alarm,
	ingestionCfg config.IngestionConfig,
	generatedAt time.Time,
) (TargetBaseline, error) {
	if err := ValidateOperationalTargets(channels); err != nil {
		return TargetBaseline{}, fmt.Errorf("build community shorts target baseline: %w", err)
	}

	activationIndex := buildAlarmActivationIndex(alarms)
	finalOwner := resolveFinalDeliveryOwner(ingestionCfg)
	cutoverPending := isCutoverPending(ingestionCfg, generatedAt)

	enabledChannels := make([]TargetBaselineChannel, 0, len(channels))
	for i := range channels {
		if !channels[i].Enabled {
			continue
		}
		channelID := strings.TrimSpace(channels[i].ChannelID)
		if channelID == "" {
			continue
		}
		targetKeys := sharedalarmkeys.BuildChannelContentAlarmTargetKeys(channelID)
		enabledChannels = append(enabledChannels, TargetBaselineChannel{
			OwnerLabel:              strings.TrimSpace(channels[i].OwnerLabel),
			ChannelID:               channelID,
			CommunitySubscribersKey: targetKeys.CommunitySubscribersKey,
			ShortsSubscribersKey:    targetKeys.ShortsSubscribersKey,
			Routes:                  buildTargetBaselineRoutes(channelID, finalOwner, activationIndex, cutoverPending),
		})
	}

	slices.SortFunc(enabledChannels, func(left, right TargetBaselineChannel) int {
		if left.ChannelID != right.ChannelID {
			return strings.Compare(left.ChannelID, right.ChannelID)
		}
		return strings.Compare(left.OwnerLabel, right.OwnerLabel)
	})

	cutoverAt := normalizedCutoverAt(ingestionCfg.CommunityShortsBigBangCutoverAt)

	return TargetBaseline{
		GeneratedAt: generatedAt.UTC(),
		Runtime: TargetBaselineRuntime{
			FinalDeliveryOwner:              finalOwner,
			CommunityShortsBigBangEnabled:   ingestionCfg.CommunityShortsBigBangEnabled,
			CommunityShortsBigBangCutoverAt: cutoverAt,
			TargetChannelCount:              len(enabledChannels),
		},
		Sources: TargetBaselineSources{
			OperationalChannels: "postgres.members -> resolveCommunityShortsOperationalChannels",
			TypedSubscriberKeys: "alarm typed subscriber keys -> BuildChannelContentAlarmTargetKeys",
			RoomSubscriptions:   "postgres.alarms alarm_types -> community/shorts typed room counts",
		},
		PathMappings: buildTargetBaselinePaths(enabledChannels, finalOwner, cutoverPending),
		Channels:     enabledChannels,
	}, nil
}

func buildAlarmActivationIndex(alarms []*domain.Alarm) map[alarmActivationKey]map[string]struct{} {
	index := make(map[alarmActivationKey]map[string]struct{})
	for _, alarmRecord := range alarms {
		if alarmRecord == nil {
			continue
		}

		roomID := strings.TrimSpace(alarmRecord.RoomID)
		channelID := strings.TrimSpace(alarmRecord.ChannelID)
		if roomID == "" || channelID == "" {
			continue
		}

		for _, alarmType := range normalizedAlarmTypes(alarmRecord.AlarmTypes) {
			key := alarmActivationKey{channelID: channelID, alarmType: alarmType}
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
