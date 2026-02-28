package domain

// TalentProfile: 크롤링 등을 통해 수집된 탤런트 상세 프로필 정보 (이름, 캐치프레이즈, 데이터 항목, 소셜 링크 등)
type TalentProfile struct {
	Slug         string               `json:"slug"`
	EnglishName  string               `json:"english_name"`
	JapaneseName string               `json:"japanese_name"`
	Catchphrase  string               `json:"catchphrase"`
	Description  string               `json:"description"`
	DataEntries  []TalentProfileEntry `json:"data_entries"`
	SocialLinks  []TalentSocialLink   `json:"social_links"`
	OfficialURL  string               `json:"official_url"`
}

// TalentProfileEntry: 프로필의 키-값 데이터 항목 (예: "생일": "1월 1일")
type TalentProfileEntry struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// TalentSocialLink: 탤런트의 소셜 미디어 링크 정보
type TalentSocialLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Translated: 번역된 프로필 정보 (표시 이름, 요약, 하이라이트 등)
type Translated struct {
	DisplayName string                     `json:"display_name"`
	Catchphrase string                     `json:"catchphrase"`
	Summary     string                     `json:"summary"`
	Highlights  []string                   `json:"highlights"`
	Data        []TranslatedProfileDataRow `json:"data"`
}

// TranslatedProfileDataRow: 번역된 프로필 데이터의 개별 행
type TranslatedProfileDataRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
