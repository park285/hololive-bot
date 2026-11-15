package scheduleposter

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

func zoneRow(day, start, end, tz string) posterstore.Row {
	return posterstore.Row{Day: utcDate(day), Start: start, End: end, TZ: tz, Status: posterstore.RowPlanned, SourceText: "配信", TitleKo: "방송"}
}

func TestLookupZoneLoadsEveryListedZone(t *testing.T) {
	t.Parallel()

	for _, label := range []string{
		"KST", "UTC", "GMT", "UTC+7", "GMT-3", "UTC+5:30", "GMT+14", "UTC-14:00", "WIB", "WITA", "WIT", "SGT", "PHT", "HKT",
		"PT", "PST", "PDT", "MT", "MST", "MDT", "CT", "CST", "CDT", "ET", "EST", "EDT", "CET", "CEST", "BST", "AET", "AEST", "AEDT",
		" pdt ", "cest",
	} {
		zone, err := lookupZone(label)
		require.NoError(t, err, label)
		require.True(t, zone.converted, label)

		if label != "KST" {
			require.NotNil(t, zone.loc, label)
		}
	}

	for _, label := range []string{"", "JST", " jst "} {
		zone, err := lookupZone(label)
		require.NoError(t, err, label)
		require.False(t, zone.converted, label)
	}

	for _, label := range []string{"IST", "UTC+15", "GMT-14:30", "UTC+7:60", "Asia/Tokyo", "PT8", "EST5EDT"} {
		_, err := lookupZone(label)
		require.ErrorIs(t, err, errUnsupportedZone, label)
	}
}

func TestLookupZoneKeysLabelsOfTheSamePlaceOrOffsetAlike(t *testing.T) {
	t.Parallel()

	key := func(label string) string {
		zone, err := lookupZone(label)
		require.NoError(t, err, label)

		return zone.key
	}

	require.Equal(t, key("PT"), key("PDT"))
	require.Equal(t, key("PST"), key("pdt"))
	require.Equal(t, key("CET"), key("CEST"))
	require.Equal(t, key("WIB"), key("UTC+7"))
	require.Equal(t, key("GMT+07:00"), key("UTC+7"))
	require.Equal(t, key("UTC"), key("GMT"))
	require.Equal(t, key("JST"), key(""))
	require.NotEqual(t, key("PT"), key("MT"))
	require.NotEqual(t, key("WIB"), key("SGT"))
}

func zoneRowOf(spec string) posterstore.Row {
	fields := strings.Fields(spec)
	start, end, _ := strings.Cut(fields[1], "-")

	return zoneRow(fields[0], start, end, fields[2])
}

func shownSlot(row posterstore.Row) string {
	return strings.TrimSpace(row.Day.Format(time.DateOnly) + " " + row.Start)
}

func TestToJSTRowsConvertsPosterTimesToJapaneseDayAndClock(t *testing.T) {
	t.Parallel()

	for spec, want := range map[string]string{
		"2026-11-03 19:00 PDT":     "2026-11-04 12:00",
		"2026-10-28 19:00 PT":      "2026-10-29 11:00",
		"2026-11-03 23:30 WIB":     "2026-11-03 25:30",
		"2026-11-03 20:00 CET":     "2026-11-03 28:00",
		"2026-10-20 20:00 CEST":    "2026-10-20 27:00",
		"2026-10-31 19:00 CDT":     "2026-11-01 09:00",
		"2026-11-03 25:00 PT":      "2026-11-04 18:00",
		"2026-11-03 9:00 UTC+5:30": "2026-11-03 12:30",
	} {
		rows, _, err := toJSTRows([]posterstore.Row{zoneRowOf(spec)})
		require.NoError(t, err, spec)
		require.Equal(t, want, shownSlot(rows[0]), spec)
		require.Equal(t, "JST", rows[0].TZ, spec)
		require.Equal(t, strings.Fields(spec)[2], rows[0].SourceZone, spec)
	}
}

func TestToJSTRowsWritesTheEndFromTheConvertedStartDay(t *testing.T) {
	t.Parallel()

	for spec, want := range map[string]string{
		"2026-11-03 19:00-21:00 PDT": "14:00",
		"2026-11-03 23:30-25:00 WIB": "27:00",
		"2026-11-03 23:30-29:00 WIB": "",
		"2026-11-03 23:00-1:00 PT":   "",
		"2026-11-03 -21:00 PT":       "",
	} {
		rows, _, err := toJSTRows([]posterstore.Row{zoneRowOf(spec)})
		require.NoError(t, err, spec)
		require.Equal(t, want, rows[0].End, spec)
	}
}

func TestToJSTRowsMovesUntimedRowsByThePosterShift(t *testing.T) {
	t.Parallel()

	rows, _, err := toJSTRows([]posterstore.Row{
		zoneRowOf("2026-11-03 19:00 PT"), zoneRowOf("2026-11-04 18:00 PT"), zoneRowOf("2026-11-05 - PT"),
	})
	require.NoError(t, err)
	require.Equal(t, utcDate("2026-11-06"), rows[2].Day, "시작 있는 변환 행의 최빈 날짜 이동을 따른다")
	require.Equal(t, "PT", rows[2].SourceZone)
	require.Equal(t, "JST", rows[2].TZ)

	rows, _, err = toJSTRows([]posterstore.Row{
		zoneRowOf("2026-11-03 19:00 PT"), zoneRowOf("2026-11-04 7:00 PT"), zoneRowOf("2026-11-05 - PT"), zoneRowOf("2026-11-05 - WIB"),
	})
	require.NoError(t, err)
	require.Equal(t, "24:00", rows[1].Start)
	require.Equal(t, utcDate("2026-11-06"), rows[2].Day, "동률이면 그날 현지 20:00의 이동을 쓴다")
	require.Equal(t, utcDate("2026-11-05"), rows[3].Day)

	rows, _, err = toJSTRows([]posterstore.Row{zoneRowOf("2026-11-05 - CET")})
	require.NoError(t, err)
	require.Equal(t, utcDate("2026-11-05"), rows[0].Day, "시작 있는 변환 행이 없어도 현지 20:00(JST 04:00, 같은 날 28:00)으로 정한다")
}

func TestToJSTRowsReadsZonelessUntimedRowsInTheZoneOfMostTimedRows(t *testing.T) {
	t.Parallel()

	rows, shift, err := toJSTRows([]posterstore.Row{
		zoneRowOf("2026-11-03 19:00 PT"), zoneRowOf("2026-11-04 19:00 PST"), zoneRow("2026-11-05", "", "", ""), zoneRow("2026-11-06", "20:00", "", ""),
	})
	require.NoError(t, err)
	require.Equal(t, 1, shift)
	require.Equal(t, utcDate("2026-11-06"), rows[2].Day, "시각 없는 칸은 시각 있는 칸 다수의 시간대를 따른다")
	require.Equal(t, "PT", rows[2].SourceZone)
	require.Equal(t, "2026-11-06 20:00", shownSlot(rows[3]), "시각 있는 칸의 빈 시간대는 JST다")
	require.Empty(t, rows[3].SourceZone)

	rows, shift, err = toJSTRows([]posterstore.Row{
		zoneRow("2026-11-09", "20:00", "", ""), zoneRow("2026-11-10", "", "", ""), zoneRow("2026-11-12", "20:00", "", ""), zoneRowOf("2026-11-13 19:00 PT"),
	})
	require.NoError(t, err)
	require.Equal(t, 1, shift)
	require.Equal(t, []string{"2026-11-09 20:00", "2026-11-10", "2026-11-12 20:00", "2026-11-14 12:00"},
		[]string{shownSlot(rows[0]), shownSlot(rows[1]), shownSlot(rows[2]), shownSlot(rows[3])}, "JST 포스터의 해외 칸 하나는 그 칸만 바꾼다")
	require.Equal(t, []string{"", "", "", "PT"}, []string{rows[0].SourceZone, rows[1].SourceZone, rows[2].SourceZone, rows[3].SourceZone})
}

func TestToJSTRowsKeepsJapaneseAndKoreanTimes(t *testing.T) {
	t.Parallel()

	rows := []posterstore.Row{
		zoneRow("2026-11-03", "25:00", "26:00", "JST"),
		zoneRow("2026-11-03", "1:00", "", ""),
		zoneRow("2026-11-03", "21:00", "", "jst"),
		zoneRow("2026-11-04", "1:00", "03:00", " KST "),
	}

	converted, _, err := toJSTRows(rows)
	require.NoError(t, err)
	require.Equal(t, rows[:3], converted[:3], "JST 행은 그대로다")
	require.Equal(t, posterstore.Row{
		Day: utcDate("2026-11-04"), Start: "1:00", End: "03:00", TZ: "JST", SourceZone: "KST", Status: posterstore.RowPlanned,
		SourceText: "配信", TitleKo: "방송",
	}, converted[3], "KST는 날짜·시각을 그대로 두고 변환 행으로 표시한다")
}

func TestRowsDayShiftCoversTheMostRows(t *testing.T) {
	t.Parallel()

	weekStart, weekEnd := utcDate("2026-11-09"), utcDate("2026-11-15")
	converted := func(day string) posterstore.Row {
		row := zoneRow(day, "12:00", "", "JST")

		row.SourceZone = "PT"

		return row
	}
	plain := func(day string) posterstore.Row { return zoneRow(day, "12:00", "", "JST") }

	tuesdayToSunday := []posterstore.Row{
		converted("2026-11-10"), converted("2026-11-11"), converted("2026-11-12"), converted("2026-11-13"), converted("2026-11-14"), converted("2026-11-15"),
	}

	for name, tc := range map[string]struct {
		rows        []posterstore.Row
		posterShift int
		want        int
	}{
		"JST only":                     {rows: []posterstore.Row{plain("2026-11-16"), plain("2026-11-16")}, posterShift: 1, want: 0},
		"later":                        {rows: []posterstore.Row{converted("2026-11-10"), converted("2026-11-16"), converted("2026-11-16")}, want: 1},
		"earlier":                      {rows: []posterstore.Row{converted("2026-11-08"), converted("2026-11-08"), converted("2026-11-14")}, want: -1},
		"tie prefers0":                 {rows: []posterstore.Row{converted("2026-11-09"), converted("2026-11-16")}, want: 0},
		"tie later":                    {rows: []posterstore.Row{converted("2026-11-07"), converted("2026-11-08"), converted("2026-11-16"), converted("2026-11-17")}, want: 1},
		"inside":                       {rows: []posterstore.Row{converted("2026-11-10"), plain("2026-11-12")}, want: 0},
		"tie follows the poster shift": {rows: tuesdayToSunday, posterShift: 1, want: 1},
		"count beats the poster shift": {rows: []posterstore.Row{converted("2026-11-09"), converted("2026-11-10")}, posterShift: 1, want: 0},
	} {
		require.Equal(t, tc.want, rowsDayShift(tc.rows, weekStart, weekEnd, tc.posterShift), name)
	}
}

func TestToJSTRowsFollowsDaylightTransitions(t *testing.T) {
	t.Parallel()

	rows, _, err := toJSTRows([]posterstore.Row{zoneRowOf("2026-10-31 20:00 PDT"), zoneRowOf("2026-11-01 20:00 PDT")})
	require.NoError(t, err)
	require.Equal(t, []string{"2026-11-01 12:00", "2026-11-02 13:00"}, []string{shownSlot(rows[0]), shownSlot(rows[1])})

	repeated, _, err := toJSTRows([]posterstore.Row{zoneRowOf("2026-10-31 25:30 PT"), zoneRowOf("2026-11-01 01:30 PT")})
	require.NoError(t, err)
	require.Equal(t, shownSlot(repeated[0]), shownSlot(repeated[1]), "겹친 시각의 심야 표기는 다음 날 같은 시각과 같다")
	require.Contains(t, []string{"2026-11-01 17:30", "2026-11-01 18:30"}, shownSlot(repeated[0]))
}
