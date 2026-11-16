package scheduleposter

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const maxMatchZoneChars = 40

// 지난 시각 검사는 칸의 날짜나 시각을 새로 정하는 판정에만 한다. 지난 방송의 취소·휴방 공지는 반영한다.
var momentKinds = []posterstore.ChangeKind{
	posterstore.ChangeAdded, posterstore.ChangeMoved, posterstore.ChangeConfirmed, posterstore.ChangeTimeChanged,
}

type matchCandidate struct {
	day    time.Time
	start  *string
	status posterstore.RowStatus
	text   string
	// target은 ApplyChanges(rows, prior)에서의 위치이고 새 칸이면 -1이다. 이 글의 변경을 더 적용해도 앞 위치는 그대로다.
	target int
}

type itemJudge struct {
	posted  string
	body    string
	lower   time.Time
	weekEnd time.Time
	base    []posterstore.Slot
	added   int
}

// judgeItems는 대응 항목마다 변경 하나를 같은 순서로 돌려주며, prior에는 이 글보다 (PostedAt, ID)가 앞선 변경만 넣어야 한다.
func judgeItems(
	post posterstore.Post, weekStart, weekEnd time.Time, rows []posterstore.Row, prior []posterstore.Change, items []matchItem,
) []posterstore.Change {
	judge := itemJudge{
		posted: post.PostedAt.In(jst).Format(matchClockLayout), body: dropSpaces(post.Body), lower: later(postedDate(post.PostedAt), weekStart), weekEnd: weekEnd,
		base: posterstore.ApplyChanges(rows, prior),
	}
	changes := make([]posterstore.Change, len(items))
	candidates := make([]matchCandidate, len(items))

	for i := range items {
		changes[i] = posterstore.Change{PostID: post.ID, Item: i + 1, PostedAt: post.PostedAt}

		candidate, reason := judge.check(&items[i])

		candidates[i] = candidate

		if reason != "" {
			judge.reject(&changes[i], candidate.target, reason)
		}
	}

	targets := map[int]int{}

	for i := range changes {
		if changes[i].Kind == "" && candidates[i].target >= 0 {
			targets[candidates[i].target]++
		}
	}

	for i := range changes {
		switch {
		case changes[i].Kind != "":
		case targets[candidates[i].target] > 1:
			judge.reject(&changes[i], candidates[i].target, rejectDuplicateRow)
		default:
			judge.compare(&changes[i], &candidates[i])
		}
	}

	judge.rejectCollisions(rows, prior, changes, candidates)

	return changes
}

func (j *itemJudge) check(item *matchItem) (matchCandidate, string) {
	candidate := matchCandidate{start: item.Start, status: item.Status, text: foldItemText(item.Text), target: -1}

	for _, step := range [...]func(*matchItem, *matchCandidate) string{j.checkFormat, j.checkSource, j.locate, j.redirect, j.checkTime, j.place} {
		if reason := step(item, &candidate); reason != "" {
			return candidate, reason
		}
	}

	return candidate, ""
}

func (j *itemJudge) checkFormat(item *matchItem, candidate *matchCandidate) string {
	day, err := time.Parse(time.DateOnly, item.Date)
	if err != nil || item.Start != nil && !validClock(*item.Start) || !item.Status.Valid() ||
		utf8.RuneCountInString(candidate.text) > posterstore.MaxChangeTextChars ||
		utf8.RuneCountInString(item.Evidence) > posterstore.MaxChangeTextChars ||
		utf8.RuneCountInString(optional(item.TZ)) > maxMatchZoneChars ||
		!storable(item.Date, optional(item.Start), optional(item.TZ), string(item.Status), item.Text, item.Evidence) {
		return rejectFormat
	}

	candidate.day = day

	return ""
}

func (j *itemJudge) checkSource(item *matchItem, candidate *matchCandidate) string {
	evidence := dropSpaces(item.Evidence)

	switch {
	case item.TZ != nil && !strings.EqualFold(*item.TZ, "JST"):
		return rejectForeignZone
	case evidence == "" || !strings.Contains(j.body, evidence):
		return rejectEvidence
	case forgesMarker(candidate.text):
		return rejectTextMarker
	default:
		return ""
	}
}

func (j *itemJudge) locate(item *matchItem, candidate *matchCandidate) string {
	if item.Row != nil {
		if *item.Row < 1 || *item.Row > len(j.base) {
			return rejectNoSuchRow
		}

		candidate.target = *item.Row - 1

		return ""
	}

	switch candidate.status {
	case posterstore.RowCancelled:
		return rejectNoSuchRow
	case posterstore.RowOff:
		if slices.ContainsFunc(j.base, func(slot posterstore.Slot) bool { return slot.Day.Equal(candidate.day) && airs(slot.Status) }) {
			return rejectNoSuchRow
		}
	case posterstore.RowPlanned, posterstore.RowTentative:
	}

	return ""
}

// redirect는 같은 날 같은 시각의 칸을 대상으로 삼는다. 지난 시각 검사가 그 칸 기준의 판정을 봐야 하므로 checkTime보다 먼저 한다.
func (j *itemJudge) redirect(_ *matchItem, candidate *matchCandidate) string {
	if candidate.target < 0 && candidate.start != nil {
		candidate.target = slices.IndexFunc(j.base, func(slot posterstore.Slot) bool {
			return slot.Start != "" && sameMoment(slot.Day, slot.Start, candidate.day, *candidate.start)
		})
	}

	return ""
}

func (j *itemJudge) checkTime(_ *matchItem, candidate *matchCandidate) string {
	start := optional(candidate.start)
	if candidate.start == nil && candidate.target >= 0 && j.base[candidate.target].Day.Equal(candidate.day) {
		start = j.base[candidate.target].Start
	}

	if day, _ := posterstore.NormalizeDayTime(candidate.day, start); day.Before(j.lower) {
		return rejectDateRange
	}

	kind := posterstore.ChangeAdded

	if candidate.target >= 0 {
		kind = j.kind(&j.base[candidate.target], candidate)
	}

	if candidate.day.After(j.weekEnd) && kind != posterstore.ChangeMoved && kind != posterstore.ChangeMovedOut {
		return rejectDateRange
	}

	if candidate.target >= 0 && !airs(candidate.status) && !onSlotDay(&j.base[candidate.target], candidate.day) {
		return rejectRowDate
	}

	if candidate.start != nil && slices.Contains(momentKinds, kind) && clockStamp(candidate.day, *candidate.start) < j.posted {
		return rejectPastTime
	}

	return ""
}

func (j *itemJudge) place(_ *matchItem, candidate *matchCandidate) string {
	switch {
	case candidate.target >= 0:
	case candidate.text == "":
		return rejectNoContent
	case len(j.base)+j.added >= posterstore.MaxRows:
		return rejectSlotLimit
	default:
		j.added++
	}

	return ""
}

// kind는 휴방·취소를 옮김보다 먼저 본다. 옮긴 방송의 상태는 planned·tentative뿐이다.
func (j *itemJudge) kind(slot *posterstore.Slot, candidate *matchCandidate) posterstore.ChangeKind {
	switch {
	case candidate.status == posterstore.RowOff:
		return unless(slot.Status == posterstore.RowOff, posterstore.ChangeOff)
	case candidate.status == posterstore.RowCancelled:
		return unless(slot.Status == posterstore.RowCancelled, posterstore.ChangeCancelled)
	case !sameDay(slot, candidate) && candidate.day.After(j.weekEnd):
		return posterstore.ChangeMovedOut
	case !sameDay(slot, candidate):
		return posterstore.ChangeMoved
	case slot.Status == posterstore.RowOff:
		return posterstore.ChangeAdded
	case slot.Status == posterstore.RowCancelled:
		return posterstore.ChangeConfirmed
	case candidate.status == posterstore.RowTentative && slot.Status != posterstore.RowTentative:
		return posterstore.ChangeTentative
	case candidate.status == posterstore.RowPlanned && slot.Status == posterstore.RowTentative:
		return posterstore.ChangeConfirmed
	case candidate.start != nil && !sameMoment(slot.Day, slot.Start, candidate.day, *candidate.start):
		return posterstore.ChangeTimeChanged
	case candidate.text != "":
		return posterstore.ChangeContentChanged
	default:
		return posterstore.ChangeSame
	}
}

func unless(same bool, kind posterstore.ChangeKind) posterstore.ChangeKind {
	if same {
		return posterstore.ChangeSame
	}

	return kind
}

func (j *itemJudge) compare(change *posterstore.Change, candidate *matchCandidate) {
	change.Text = candidate.text

	if candidate.target < 0 {
		change.Kind, change.Day, change.Start, change.Status = posterstore.ChangeAdded, candidate.day, orEmpty(candidate.start), candidate.status

		return
	}

	slot := &j.base[candidate.target]

	change.Kind = j.kind(slot, candidate)
	setTarget(change, slot)

	switch change.Kind {
	case posterstore.ChangeOff:
		change.Start, change.Status = orEmpty(nil), posterstore.RowOff
	case posterstore.ChangeCancelled:
		change.Status = posterstore.RowCancelled
	case posterstore.ChangeMovedOut:
		change.Day, change.Start, change.Status = candidate.day, candidate.start, candidate.status
	case posterstore.ChangeMoved:
		change.Day, change.Start, change.Status = candidate.day, orEmpty(candidate.start), candidate.status
	case posterstore.ChangeAdded, posterstore.ChangeConfirmed, posterstore.ChangeTentative, posterstore.ChangeTimeChanged:
		change.Start, change.Status = candidate.start, candidate.status
		if change.Kind == posterstore.ChangeTentative {
			change.Start = orEmpty(candidate.start)
		}

		if change.Kind == posterstore.ChangeTimeChanged {
			change.Status = ""
		}

		if candidate.start != nil && !candidate.day.Equal(slot.Day) {
			change.Day = candidate.day
		}
	case posterstore.ChangeSame:
		sameValues(change, slot, candidate)
	case posterstore.ChangeContentChanged, posterstore.ChangeRejected:
	}
}

func sameValues(change *posterstore.Change, slot *posterstore.Slot, candidate *matchCandidate) {
	switch candidate.status {
	case posterstore.RowOff:
		change.Start, change.Status = orEmpty(nil), posterstore.RowOff
	case posterstore.RowCancelled:
		change.Status = posterstore.RowCancelled
	case posterstore.RowPlanned, posterstore.RowTentative:
		change.Day, change.Status = slot.Day, candidate.status
		if candidate.start != nil {
			change.Start = new(slot.Start)
		}
	}
}

func (j *itemJudge) rejectCollisions(rows []posterstore.Row, prior, changes []posterstore.Change, candidates []matchCandidate) {
	for {
		hits := j.collisions(posterstore.ApplyChanges(rows, append(slices.Clip(prior), changes...)), changes, candidates)
		if len(hits) == 0 {
			return
		}

		for _, i := range hits {
			j.reject(&changes[i], candidates[i].target, rejectTimeCollision)
		}
	}
}

func (j *itemJudge) collisions(slots []posterstore.Slot, changes []posterstore.Change, candidates []matchCandidate) []int {
	var hits []int

	for i := range changes {
		change := &changes[i]
		if !change.Kind.Applied() || change.Kind == posterstore.ChangeMovedOut {
			continue
		}

		at := candidates[i].target
		if at < 0 {
			at = slices.IndexFunc(slots, func(slot posterstore.Slot) bool {
				return slot.AddedPostID == change.PostID && slot.AddedItem == change.Item
			})
		}

		after := &slots[at]
		if after.Start == "" || candidates[i].target >= 0 && j.unmoved(at, after) {
			continue
		}

		for k := range slots {
			if k != at && airs(slots[k].Status) && slots[k].Start != "" && sameMoment(slots[k].Day, slots[k].Start, after.Day, after.Start) {
				hits = append(hits, i)

				break
			}
		}
	}

	return hits
}

func (j *itemJudge) unmoved(at int, after *posterstore.Slot) bool {
	before := &j.base[at]

	return airs(before.Status) && sameMoment(before.Day, before.Start, after.Day, after.Start)
}

func (j *itemJudge) reject(change *posterstore.Change, target int, reason string) {
	*change = posterstore.Change{
		PostID: change.PostID, Item: change.Item, PostedAt: change.PostedAt, Kind: posterstore.ChangeRejected, Reason: reason,
	}

	if target >= 0 && j.base[target].AddedPostID == 0 {
		change.Slot = j.base[target].Ordinal
	}
}

func setTarget(change *posterstore.Change, slot *posterstore.Slot) {
	if slot.AddedPostID == 0 {
		change.Slot = slot.Ordinal

		return
	}

	change.TargetPostID, change.TargetItem = slot.AddedPostID, slot.AddedItem
}

func airs(status posterstore.RowStatus) bool {
	return status == posterstore.RowPlanned || status == posterstore.RowTentative
}

func orEmpty(start *string) *string {
	if start != nil {
		return start
	}

	return new("")
}

func sameDay(slot *posterstore.Slot, candidate *matchCandidate) bool {
	if candidate.start == nil {
		return onSlotDay(slot, candidate.day)
	}

	if candidate.day.Equal(slot.Day) {
		return true
	}

	slotDay, _ := posterstore.NormalizeDayTime(slot.Day, slot.Start)
	itemDay, _ := posterstore.NormalizeDayTime(candidate.day, *candidate.start)

	return slotDay.Equal(itemDay)
}

func onSlotDay(slot *posterstore.Slot, day time.Time) bool {
	slotDay, _ := posterstore.NormalizeDayTime(slot.Day, slot.Start)

	return day.Equal(slot.Day) || day.Equal(slotDay)
}

func sameMoment(dayA time.Time, startA string, dayB time.Time, startB string) bool {
	dayA, startA = posterstore.NormalizeDayTime(dayA, startA)
	dayB, startB = posterstore.NormalizeDayTime(dayB, startB)

	return dayA.Equal(dayB) && startA == startB
}

// clockStamp는 검증한 시각을 게시 시각과 같은 "YYYY-MM-DD HH:MM" 꼴로 맞춰, 문자열 순서가 시간 순서가 되게 한다.
func clockStamp(day time.Time, start string) string {
	day, start = posterstore.NormalizeDayTime(day, start)

	return day.Format(time.DateOnly) + " " + start
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// 모델은 근거를 옮기며 본문의 줄바꿈·공백을 빼거나 바꾸므로 공백을 모두 뺀 문자열로 견준다.
func dropSpaces(text string) string {
	return strings.Join(strings.Fields(text), "")
}

// foldItemText는 text가 대응·번역 지시문의 한 줄에 들어가므로 줄바꿈을 " / "로 접고 다른 제어 문자를 공백으로 바꾼다.
func foldItemText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.NewReplacer("\r\n", " / ", "\n", " / ", "\r", " / ", "\u2028", " / ", "\u2029", " / ").Replace(text)

	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, text)
}

func storable(texts ...string) bool {
	return !slices.ContainsFunc(texts, func(text string) bool { return !utf8.ValidString(text) || strings.ContainsRune(text, 0) })
}
