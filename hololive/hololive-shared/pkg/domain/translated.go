package domain

import "embed"

//go:embed data/official_profiles_ko/*.json
var officialProfilesKoFS embed.FS

var translatedProfilesCache profileCache[Translated]

// LoadTranslated: 번역된 프로필 파일(official_profiles_ko 디렉토리)을 읽어 메모리에 로드합니다.
func LoadTranslated() (map[string]*Translated, error) {
	return loadEmbeddedProfiles(
		&translatedProfilesCache,
		officialProfilesKoFS,
		"data/official_profiles_ko",
		"translated profiles",
		"translated profile",
		true,
		nil,
	)
}
