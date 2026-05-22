package settings

import "time"

type HolodexConfig struct {
	BaseURL string
	APIKey  string
}

type YouTubeConfig struct {
	APIKey              string
	EnableQuotaBuilding bool
}

type IngestionConfig struct {
	YouTubeEnabled                  bool
	PhotoSyncEnabled                bool
	CommunityShortsBigBangEnabled   bool
	CommunityShortsBigBangCutoverAt time.Time
}

type ChzzkConfig struct {
	ClientID     string
	ClientSecret string
}

type TwitchConfig struct {
	ClientID     string
	ClientSecret string
}
