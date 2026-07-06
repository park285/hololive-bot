package broadcasttype

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

type Type string

const (
	Game        Type = "game"
	Talk        Type = "talk"
	Singing     Type = "singing"
	ASMR        Type = "asmr"
	Membership  Type = "membership"
	Event       Type = "event"
	HorseRacing Type = "horse_racing"
	Watchalong  Type = "watchalong"
	News        Type = "news"
	Other       Type = "other"
	Unknown     Type = "unknown"
)

var aliases = map[string]Type{
	"game": Game, "games": Game, "gaming": Game, "게임": Game, "겜": Game, "게임방송": Game,
	"talk": Talk, "zatsudan": Talk, "free_talk": Talk, "free-talk": Talk, "잡담": Talk, "토크": Talk, "수다": Talk,
	"singing": Singing, "song": Singing, "karaoke": Singing, "music": Singing, "노래": Singing, "노래방": Singing, "歌枠": Singing, "우타와꾸": Singing,
	"asmr":       ASMR,
	"membership": Membership, "member": Membership, "members": Membership, "membersonly": Membership, "memberonly": Membership, "멤버십": Membership, "멤버": Membership, "멤버한정": Membership, "멤버전용": Membership,
	"event": Event, "events": Event, "birthday": Event, "3d": Event, "outfit": Event, "이벤트": Event, "기념": Event, "생일": Event, "신의상": Event, "3d방송": Event,
	"horse_racing": HorseRacing, "horse-racing": HorseRacing, "horseracing": HorseRacing, "keiba": HorseRacing, "경마": HorseRacing, "競馬": HorseRacing,
	"watchalong": Watchalong, "watch-along": Watchalong, "watch-party": Watchalong, "watch_party": Watchalong, "watchparty": Watchalong, "동시시청": Watchalong, "같이보기": Watchalong,
	"news": News, "notice": News, "announcement": News, "뉴스": News, "공지": News,
	"other": Other, "variety": Other, "etc": Other, "기타": Other,
	"unknown": Unknown, "미분류": Unknown,
}

var labels = map[Type]string{
	Game:        "게임",
	Talk:        "잡담",
	Singing:     "노래",
	ASMR:        "ASMR",
	Membership:  "멤버십",
	Event:       "이벤트",
	HorseRacing: "경마",
	Watchalong:  "동시시청",
	News:        "뉴스/공지",
	Other:       "기타",
}

func Parse(raw string) (Type, bool) {
	typ, ok := aliases[NormalizeToken(raw)]
	return typ, ok
}

func IsAlias(raw string) bool {
	_, ok := Parse(raw)
	return ok
}

func (t Type) Label() string {
	if label, ok := labels[t]; ok {
		return label
	}
	return "미분류"
}

func Known(typ Type) bool {
	switch typ {
	case Game,
		Talk,
		Singing,
		ASMR,
		Membership,
		Event,
		HorseRacing,
		Watchalong,
		News,
		Other,
		Unknown:
		return true
	default:
		return false
	}
}

func NormalizeToken(value string) string {
	value = norm.NFKC.String(value)
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "#")
	return strings.ReplaceAll(value, " ", "_")
}
