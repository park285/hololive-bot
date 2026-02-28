package domain

import (
	_ "embed" // 인재 데이터 임베드용
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// OfficialTalent: 공식 홈페이지 등에서 수집된 탤런트 상세 정보 (영문명, 링크, 상태 등)
type OfficialTalent struct {
	Japanese string `json:"japanese"`
	English  string `json:"english"`
	Link     string `json:"link"`
	Status   string `json:"status"`
}

// Talents: 전체 탤런트 목록 및 영문 이름 기반 인덱스를 관리하는 컨테이너
type Talents struct {
	Talents   []*OfficialTalent
	byEnglish map[string]*OfficialTalent
}

//go:embed data/official_talents.json
var officialTalentsJSON []byte

// LoadTalents: 임베딩된 JSON 데이터(official_talents.json)를 로드하여 Talents 객체를 초기화합니다.
func LoadTalents() (*Talents, error) {
	var talents []*OfficialTalent
	if err := json.Unmarshal(officialTalentsJSON, &talents); err != nil {
		return nil, fmt.Errorf("failed to unmarshal talents data: %w", err)
	}

	index := make(map[string]*OfficialTalent, len(talents))
	for _, talent := range talents {
		if talent == nil {
			continue
		}
		index[stringutil.Normalize(talent.English)] = talent
	}

	return &Talents{
		Talents:   talents,
		byEnglish: index,
	}, nil
}

// FindByEnglish: 영문 이름을 기준으로 탤런트 정보를 검색한다. (이름 정규화 적용)
func (ot *Talents) FindByEnglish(name string) *OfficialTalent {
	if ot == nil {
		return nil
	}
	return ot.byEnglish[stringutil.Normalize(name)]
}

// Slug: 탤런트의 고유 식별자(Slug)를 생성한다. 공식 프로필 링크의 경로(path)를 우선 사용하고, 없으면 영문 이름을 변환하여 사용합니다.
func (ot *OfficialTalent) Slug() string {
	if ot == nil {
		return ""
	}

	if ot.Link == "" {
		return ""
	}

	u, err := url.Parse(ot.Link)
	if err == nil {
		segment := strings.Trim(path.Base(u.Path), "/")
		if segment != "" && segment != "." && segment != "/" {
			return segment
		}
	}

	return stringutil.Slugify(ot.English)
}
