package domain

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

//go:embed data/official_profiles_raw/*.json
var officialProfilesRawFS embed.FS

type profileCache[T any] struct {
	once sync.Once
	data map[string]*T
	err  error
}

var rawProfilesCache profileCache[TalentProfile]

// LoadProfiles: data/official_profiles_raw 디렉토리에 임베딩된 JSON 파일들을 읽어 멤버별 심층 프로필 정보를 로드합니다.
func LoadProfiles() (map[string]*TalentProfile, error) {
	return loadEmbeddedProfiles(
		&rawProfilesCache,
		officialProfilesRawFS,
		"data/official_profiles_raw",
		"profiles",
		"profile",
		false,
		func(slug string, profile *TalentProfile) {
			if profile.Slug == "" {
				profile.Slug = slug
			}
		},
	)
}

func loadEmbeddedProfiles[T any](
	cache *profileCache[T],
	fsys fs.FS,
	dir string,
	collectionLabel string,
	itemLabel string,
	allowEmpty bool,
	after func(slug string, profile *T),
) (map[string]*T, error) {
	cache.once.Do(func() {
		cache.data, cache.err = readEmbeddedProfiles(fsys, dir, collectionLabel, itemLabel, allowEmpty, after)
	})
	if cache.err != nil {
		return nil, cache.err
	}
	return cache.data, nil
}

func readEmbeddedProfiles[T any](
	fsys fs.FS,
	dir string,
	collectionLabel string,
	itemLabel string,
	allowEmpty bool,
	after func(slug string, profile *T),
) (map[string]*T, error) {
	files, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded %s: %w", collectionLabel, err)
	}
	if len(files) == 0 {
		if allowEmpty {
			return map[string]*T{}, nil
		}
		return nil, fmt.Errorf("no embedded %s found", collectionLabel)
	}

	profiles := make(map[string]*T, len(files))
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		slug := strings.TrimSuffix(file.Name(), path.Ext(file.Name()))
		data, err := fs.ReadFile(fsys, path.Join(dir, file.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read %s %s: %w", itemLabel, file.Name(), err)
		}

		var profile T
		if err := json.Unmarshal(data, &profile); err != nil {
			return nil, fmt.Errorf("failed to parse %s %s: %w", itemLabel, file.Name(), err)
		}

		if after != nil {
			after(slug, &profile)
		}
		profiles[slug] = &profile
	}

	return profiles, nil
}
