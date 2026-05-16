// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package model

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
	files, err := readEmbeddedProfileEntries(fsys, dir, collectionLabel, allowEmpty)
	if err != nil {
		return nil, err
	}

	profiles := make(map[string]*T, len(files))
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		slug, profile, err := readEmbeddedProfile(fsys, dir, itemLabel, file.Name(), after)
		if err != nil {
			return nil, err
		}
		profiles[slug] = &profile
	}

	return profiles, nil
}

func readEmbeddedProfileEntries(fsys fs.FS, dir string, collectionLabel string, allowEmpty bool) ([]fs.DirEntry, error) {
	files, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded %s: %w", collectionLabel, err)
	}
	if len(files) == 0 && !allowEmpty {
		return nil, fmt.Errorf("no embedded %s found", collectionLabel)
	}
	return files, nil
}

func readEmbeddedProfile[T any](
	fsys fs.FS,
	dir string,
	itemLabel string,
	filename string,
	after func(slug string, profile *T),
) (string, T, error) {
	slug := strings.TrimSuffix(filename, path.Ext(filename))
	data, err := fs.ReadFile(fsys, path.Join(dir, filename))
	if err != nil {
		var zero T
		return "", zero, fmt.Errorf("failed to read %s %s: %w", itemLabel, filename, err)
	}

	var profile T
	if err := json.Unmarshal(data, &profile); err != nil {
		return "", profile, fmt.Errorf("failed to parse %s %s: %w", itemLabel, filename, err)
	}

	if after != nil {
		after(slug, &profile)
	}
	return slug, profile, nil
}
