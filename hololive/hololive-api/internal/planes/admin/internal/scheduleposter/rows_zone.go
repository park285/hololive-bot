package scheduleposter

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	jstZoneKey       = "JST"
	koreanZoneLabel  = "KST"
	lateNightHours   = 6
	maxZoneOffset    = 14 * time.Hour
	untimedProbeHour = 20
)

var (
	fixedZoneHours  = map[string]int{"UTC": 0, "GMT": 0, "WIB": 7, "WITA": 8, "WIT": 9, "SGT": 8, "PHT": 8, "HKT": 8}
	zoneOffsetLabel = regexp.MustCompile(`^(?:UTC|GMT)([+-])(\d{1,2})(?::([0-5]\d))?$`)
	// 포스터가 여름에 PST라고 적어도 멤버의 현지 시각을 뜻하므로 표준·일광 표기를 모두 그날의 지역 시각으로 읽는다.
	regionZones = []struct {
		key, location string
		labels        []string
	}{
		{"PT", "America/Los_Angeles", []string{"PT", "PST", "PDT"}},
		{"MT", "America/Denver", []string{"MT", "MST", "MDT"}},
		{"CT", "America/Chicago", []string{"CT", "CST", "CDT"}},
		{"ET", "America/New_York", []string{"ET", "EST", "EDT"}},
		{"CET", "Europe/Berlin", []string{"CET", "CEST"}},
		{"BST", "Europe/London", []string{"BST"}},
		{"AET", "Australia/Sydney", []string{"AET", "AEST", "AEDT"}},
	}
	regionLocations = sync.OnceValues(func() (map[string]*time.Location, error) {
		locations := make(map[string]*time.Location, len(regionZones))

		for _, region := range regionZones {
			location, err := time.LoadLocation(region.location)
			if err != nil {
				return nil, fmt.Errorf("load time zone %s: %w", region.location, err)
			}

			locations[region.key] = location
		}

		return locations, nil
	})
)

// rowZone의 loc이 nil이면 날짜·시각을 그대로 둔다(JST·KST). 다수결은 key로 견주며, 같은 지역이나 같은 시차를 뜻하는 표기는 key가 같다.
type rowZone struct {
	key       string
	loc       *time.Location
	converted bool
}

// lookupZone은 표에 없는 시간대에 errUnsupportedZone을 돌려준다. 시간대 데이터를 읽지 못한 오류는 그와 구분된다.
func lookupZone(tz string) (rowZone, error) {
	label := strings.ToUpper(strings.TrimSpace(tz))

	switch label {
	case "", jstZoneKey:
		return rowZone{key: jstZoneKey}, nil
	case koreanZoneLabel:
		return rowZone{key: jstZoneKey, converted: true}, nil
	}

	if hours, ok := fixedZoneHours[label]; ok {
		return fixedRowZone(time.Duration(hours) * time.Hour), nil
	}

	if offset, ok := labelOffset(label); ok {
		return fixedRowZone(offset), nil
	}

	for _, region := range regionZones {
		if !slices.Contains(region.labels, label) {
			continue
		}

		locations, err := regionLocations()
		if err != nil {
			return rowZone{}, err
		}

		return rowZone{key: region.key, loc: locations[region.key], converted: true}, nil
	}

	return rowZone{}, errUnsupportedZone
}

func labelOffset(label string) (time.Duration, bool) {
	match := zoneOffsetLabel.FindStringSubmatch(label)
	if match == nil {
		return 0, false
	}

	hours, hourErr := strconv.Atoi(match[2])
	minutes, minuteErr := strconv.Atoi(cmp.Or(match[3], "0"))

	offset := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
	if hourErr != nil || minuteErr != nil || offset > maxZoneOffset {
		return 0, false
	}

	if match[1] == "-" {
		offset = -offset
	}

	return offset, true
}

func fixedRowZone(offset time.Duration) rowZone {
	key := jstZoneKey

	if offset != 9*time.Hour {
		sign := "+"

		if offset < 0 {
			sign = "-"
		}

		minutes := int(offset.Abs().Minutes())

		key = fmt.Sprintf("UTC%s%02d:%02d", sign, minutes/60, minutes%60)
	}

	return rowZone{key: key, loc: time.FixedZone(key, int(offset.Seconds())), converted: true}
}

func toJSTRows(rows []posterstore.Row) (converted []posterstore.Row, posterShift int, err error) {
	untimed, err := untimedZone(rows)
	if err != nil {
		return nil, 0, err
	}

	converted = make([]posterstore.Row, len(rows))
	copy(converted, rows)

	zones := make([]rowZone, len(rows))
	labels := make([]string, len(rows))
	shifts := map[int]int{}

	for i := range converted {
		labels[i] = strings.TrimSpace(converted[i].TZ)
		if labels[i] == "" && converted[i].Start == "" {
			labels[i] = untimed
		}

		zone, err := lookupZone(labels[i])
		if err != nil {
			return nil, 0, fmt.Errorf("convert row %d zone %q: %w", i+1, clip(labels[i]), err)
		}

		zones[i] = zone

		row := &converted[i]
		if !zone.converted || row.Start == "" {
			continue
		}

		local := row.Day

		if err := zone.convertTimes(row); err != nil {
			return nil, 0, fmt.Errorf("convert row %d: %w", i+1, err)
		}

		shifts[daysBetween(local, row.Day)]++
	}

	posterShift, agreed := modeShift(shifts)

	for i := range converted {
		row, zone := &converted[i], zones[i]
		if !zone.converted {
			continue
		}

		if row.Start == "" {
			move := posterShift

			if !agreed {
				probe, _ := zone.jstSlot(row.Day, untimedProbeHour*60)

				move = daysBetween(row.Day, probe)
			}

			row.Day, row.End = row.Day.AddDate(0, 0, move), ""
		}

		row.SourceZone, row.TZ = labels[i], jstZoneKey
	}

	if !agreed {
		posterShift = 0
	}

	return converted, posterShift, nil
}

// untimedZone은 시각 없는 칸의 빈 시간대를 시각 있는 칸 다수의 시간대로 정한다. 추출 지시문이 시각 없는 칸의 시간대를 비우게 하고,
// 시각 있는 칸의 빈 시간대는 포스터가 시간대를 적지 않은 JST이기 때문이다.
func untimedZone(rows []posterstore.Row) (string, error) {
	type seenZone struct{ key, label string }

	counts := map[string]int{}
	seen := make([]seenZone, 0, len(rows))

	for i := range rows {
		if rows[i].Start == "" {
			continue
		}

		label := strings.TrimSpace(rows[i].TZ)

		zone, err := lookupZone(label)
		if err != nil {
			return "", fmt.Errorf("convert row %d zone %q: %w", i+1, clip(label), err)
		}

		if counts[zone.key] == 0 {
			seen = append(seen, seenZone{key: zone.key, label: label})
		}

		counts[zone.key]++
	}

	best, chosen := counts[jstZoneKey], ""

	for _, z := range seen {
		if counts[z.key] > best {
			best, chosen = counts[z.key], z.label
		}
	}

	return chosen, nil
}

func modeShift(shifts map[int]int) (int, bool) {
	best, count, tied := 0, 0, false

	for shift, n := range shifts {
		switch {
		case n > count:
			best, count, tied = shift, n, false
		case n == count:
			tied = true
		}
	}

	return best, count > 0 && !tied
}

func clockMinutes(clock string) (int, error) {
	hour, minute, ok := strings.Cut(clock, ":")
	if !ok {
		return 0, fmt.Errorf("time %q is not H:MM", clip(clock))
	}

	h, hourErr := strconv.Atoi(hour)
	m, minuteErr := strconv.Atoi(minute)

	if err := errors.Join(hourErr, minuteErr); err != nil {
		return 0, fmt.Errorf("parse time %q: %w", clip(clock), err)
	}

	return h*60 + m, nil
}

func (z rowZone) convertTimes(row *posterstore.Row) error {
	if z.loc == nil {
		return nil
	}

	start, err := clockMinutes(row.Start)
	if err != nil {
		return err
	}

	end := -1

	if row.End != "" {
		if end, err = clockMinutes(row.End); err != nil {
			return err
		}
	}

	day, shownStart := z.jstSlot(row.Day, start)

	row.End = z.jstEnd(row.Day, start, end, day)
	row.Day, row.Start = day, shownStart

	return nil
}

func (z rowZone) instant(day time.Time, minutes int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, minutes, 0, 0, z.loc)
}

func (z rowZone) jstSlot(day time.Time, minutes int) (time.Time, string) {
	if z.loc == nil {
		return day, ""
	}

	at := z.instant(day, minutes).In(jst)
	shown := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	hour := at.Hour()

	if hour < lateNightHours {
		shown, hour = shown.AddDate(0, 0, -1), hour+24
	}

	return shown, fmt.Sprintf("%02d:%02d", hour, at.Minute())
}

func (z rowZone) jstEnd(day time.Time, start, end int, shown time.Time) string {
	if end < 0 {
		return ""
	}

	from, until := z.instant(day, start), z.instant(day, end)
	if until.Before(from) {
		return ""
	}

	minutes := int(until.Sub(time.Date(shown.Year(), shown.Month(), shown.Day(), 0, 0, 0, 0, jst)).Minutes())
	if minutes > maxRowHour*60+59 {
		return ""
	}

	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}

// rowsDayShift는 동률이면 posterShift를 먼저 고른다. 시각 있는 칸이 모두 하루 밀린 주가 창 안에 함께 들어가도 창을 밀어야 경계일에 앞 주 포스터의 밀린 칸이 보인다.
func rowsDayShift(rows []posterstore.Row, weekStart, weekEnd time.Time, posterShift int) int {
	if !slices.ContainsFunc(rows, func(row posterstore.Row) bool { return row.SourceZone != "" }) {
		return 0
	}

	order := []int{0, 1, -1}
	if slices.Contains(order, posterShift) {
		order = append([]int{posterShift}, slices.DeleteFunc(order, func(shift int) bool { return shift == posterShift })...)
	}

	best, bestCount := 0, -1

	for _, shift := range order {
		from, to := weekStart.AddDate(0, 0, shift), weekEnd.AddDate(0, 0, shift)

		count := 0

		for i := range rows {
			if !rows[i].Day.Before(from) && !rows[i].Day.After(to) {
				count++
			}
		}

		if count > bestCount {
			best, bestCount = shift, count
		}
	}

	return best
}
