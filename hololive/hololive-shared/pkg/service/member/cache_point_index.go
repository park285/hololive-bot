package member

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type memberSnapshotIndex struct {
	channelRepresentatives map[string]*domain.Member
	byName                 map[string]*domain.Member
	pointAliasByName       map[string]*domain.Member
	pointAliasByExact      map[string]*domain.Member
	channelIDs             []string
}

func newMemberSnapshotIndex(members []*domain.Member) ([]*domain.Member, *memberSnapshotIndex) {
	snapshot := make([]*domain.Member, 0, len(members))
	index := &memberSnapshotIndex{
		byName:            make(map[string]*domain.Member, len(members)),
		pointAliasByName:  make(map[string]*domain.Member, len(members)*3),
		pointAliasByExact: make(map[string]*domain.Member, len(members)*2),
		channelIDs:        make([]string, 0, len(members)),
	}

	for _, member := range members {
		if member == nil {
			continue
		}

		snapshot = append(snapshot, member)

		if member.ChannelID != "" {
			index.channelIDs = append(index.channelIDs, member.ChannelID)
		}

		keepSmallestID(index.byName, member.Name, member)
		index.addPointAliases(member)
	}

	index.channelRepresentatives = ChannelRepresentatives(snapshot)

	return snapshot, index
}

func (index *memberSnapshotIndex) addPointAliases(member *domain.Member) {
	for _, name := range [...]string{member.Name, member.NameJa, member.NameKo} {
		keepSmallestID(index.pointAliasByName, foldKey(name), member)
	}

	if member.Aliases == nil {
		return
	}

	for _, alias := range member.Aliases.Ko {
		keepSmallestID(index.pointAliasByExact, alias, member)
	}

	for _, alias := range member.Aliases.Ja {
		keepSmallestID(index.pointAliasByExact, alias, member)
	}
}

func keepSmallestID(index map[string]*domain.Member, key string, member *domain.Member) {
	if current := index[key]; current == nil || member.ID < current.ID {
		index[key] = member
	}
}

func (index *memberSnapshotIndex) aliasOwner(alias string) *domain.Member {
	byName := index.pointAliasByName[foldKey(alias)]
	byAlias := index.pointAliasByExact[alias]

	if byName == nil || (byAlias != nil && byAlias.ID < byName.ID) {
		return byAlias
	}

	return byName
}

func foldKey(value string) string {
	if foldKeyStable(value) {
		return value
	}

	var builder strings.Builder

	builder.Grow(len(value))

	for _, r := range value {
		builder.WriteRune(foldRune(r))
	}

	return builder.String()
}

func foldKeyStable(value string) bool {
	for index, r := range value {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(value[index:]); size == 1 {
				return false
			}
		}

		if foldRune(r) != r {
			return false
		}
	}

	return true
}

func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'a' <= r && r <= 'z' {
			return r - ('a' - 'A')
		}

		return r
	}

	smallest := r
	for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
		smallest = min(smallest, folded)
	}

	return smallest
}
