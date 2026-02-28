// Package flagx: 엔티티 플래그 관리를 위한 유틸리티 패키지입니다.
// Junction Table 방식의 DB 플래그 시스템과 함께 사용합니다.
package flagx

import (
	"errors"
	"slices"
)

// ErrEmptyFlag: 빈 플래그 에러입니다.
var ErrEmptyFlag = errors.New("flagx: flag cannot be empty")

// Flag: 엔티티에 부착할 수 있는 플래그입니다.
type Flag string

// Validate: 플래그가 유효한지 검사합니다.
// 빈 문자열인 경우 ErrEmptyFlag를 반환합니다.
func (f Flag) Validate() error {
	if f == "" {
		return ErrEmptyFlag
	}
	return nil
}

// String: 플래그를 문자열로 반환합니다.
func (f Flag) String() string {
	return string(f)
}

// FlagSet: 플래그의 집합입니다.
// map을 사용하여 O(1) 조회 성능을 제공합니다.
type FlagSet map[Flag]struct{}

// NewFlagSet: 새 FlagSet을 생성합니다.
// 초기 플래그들을 인자로 받아 추가합니다.
func NewFlagSet(flags ...Flag) FlagSet {
	fs := make(FlagSet, len(flags))
	for _, f := range flags {
		fs[f] = struct{}{}
	}
	return fs
}

// Add: 플래그를 추가합니다.
// 이미 존재하는 플래그를 추가해도 안전합니다 (멱등성).
func (fs FlagSet) Add(flag Flag) {
	fs[flag] = struct{}{}
}

// Remove: 플래그를 제거합니다.
// 존재하지 않는 플래그를 제거해도 안전합니다 (멱등성).
func (fs FlagSet) Remove(flag Flag) {
	delete(fs, flag)
}

// Has: 플래그 존재 여부를 반환합니다.
func (fs FlagSet) Has(flag Flag) bool {
	_, ok := fs[flag]
	return ok
}

// List: 모든 플래그를 슬라이스로 반환합니다.
// 결과는 알파벳순으로 정렬됩니다.
func (fs FlagSet) List() []Flag {
	if len(fs) == 0 {
		return nil
	}
	result := make([]Flag, 0, len(fs))
	for f := range fs {
		result = append(result, f)
	}
	slices.Sort(result)
	return result
}

// Len: 플래그 개수를 반환합니다.
func (fs FlagSet) Len() int {
	return len(fs)
}
