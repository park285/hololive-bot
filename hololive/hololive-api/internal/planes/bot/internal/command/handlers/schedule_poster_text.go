package handlers

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
)

func scheduleText(member string, schedule *scheduleposter.PublishedSchedule) string {
	slots := schedule.Slots()
	ordered := make([]*scheduleposter.Slot, len(slots))

	for i := range slots {
		ordered[i] = &slots[i]
	}

	slices.SortStableFunc(ordered, func(a, b *scheduleposter.Slot) int {
		if day := a.Day.Compare(b.Day); day != 0 {
			return day
		}

		return cmp.Compare(scheduleClockOrder(a.Start), scheduleClockOrder(b.Start))
	})

	var out strings.Builder

	fmt.Fprintf(&out, "%s 일정표 · %s ~ %s\n", member, schedule.WeekStart.AddDate(0, 0, schedule.RowsDayShift).Format(time.DateOnly), schedule.WeekEnd.AddDate(0, 0, schedule.RowsDayShift).Format(time.DateOnly))

	var day time.Time

	for _, slot := range ordered {
		if !slot.Day.Equal(day) {
			day = slot.Day
			fmt.Fprintf(&out, "\n%s\n", day.Format(time.DateOnly))
		}

		writeScheduleSlot(&out, slot)
	}

	return out.String()
}

func scheduleClockOrder(clock string) string {
	if clock == "" {
		return "99:99"
	}

	if len(clock) == 4 {
		return "0" + clock
	}

	return clock
}

func writeScheduleSlot(out *strings.Builder, slot *scheduleposter.Slot) {
	clock := slot.Start
	if clock == "" {
		clock = "시각 미정"
	}

	if slot.End != "" {
		clock += "–" + slot.End
	}

	switch slot.Status {
	case scheduleposter.RowTentative:
		clock += " · 미확정"
	case scheduleposter.RowCancelled:
		clock += " · 취소"
	case scheduleposter.RowOff:
		clock += " · 휴방"
	case scheduleposter.RowPlanned:
	}

	fmt.Fprintf(out, "%s  %s\n", clock, slot.TitleKo)

	if slot.TimeChanged {
		fmt.Fprintf(out, "  변경 전: %s %s\n", slot.OrigDay.Format(time.DateOnly), slot.OrigStart)
	}

	if !slot.MovedTo.IsZero() {
		fmt.Fprintf(out, "  이동: %s\n", slot.MovedTo.Format(time.DateOnly))
	}

	if len(slot.WithKo) > 0 {
		fmt.Fprintf(out, "  함께: %s\n", strings.Join(slot.WithKo, ", "))
	}

	if slot.PlaceKo != "" {
		fmt.Fprintf(out, "  장소: %s\n", slot.PlaceKo)
	}

	if len(slot.TagsKo) > 0 {
		fmt.Fprintf(out, "  태그: %s\n", strings.Join(slot.TagsKo, ", "))
	}

	if slot.NoteKo != "" {
		fmt.Fprintf(out, "  비고: %s\n", slot.NoteKo)
	}
}
