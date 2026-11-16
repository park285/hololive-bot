package scheduleposter

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// PostStatus는 포스터 게시 뒤 멤버가 올린 글의 처리 상태이며 migration 020의 CHECK와 같은 값이다.
type PostStatus string

const (
	PostUnjudged    PostStatus = "unjudged"
	PostNotRelevant PostStatus = "not_relevant"
	PostJudgeFailed PostStatus = "judge_failed"
	PostRelevant    PostStatus = "relevant"
	PostMatched     PostStatus = "matched"
	PostMatchFailed PostStatus = "match_failed"
	PostRevoked     PostStatus = "revoked"
)

func (s PostStatus) Valid() bool {
	switch s {
	case PostUnjudged, PostNotRelevant, PostJudgeFailed, PostRelevant, PostMatched, PostMatchFailed, PostRevoked:
		return true
	default:
		return false
	}
}

// ChangeKind는 글의 한 항목을 대상 칸과 견준 판정이며 migration 020의 CHECK와 같은 값이다.
type ChangeKind string

const (
	ChangeSame           ChangeKind = "same"
	ChangeTimeChanged    ChangeKind = "time_changed"
	ChangeConfirmed      ChangeKind = "confirmed"
	ChangeTentative      ChangeKind = "tentative"
	ChangeCancelled      ChangeKind = "cancelled" //nolint:misspell // DB CHECK의 kind 계약값이 영국식 cancelled라, canceled로 바꾸면 저장이 거부된다.
	ChangeOff            ChangeKind = "off"
	ChangeAdded          ChangeKind = "added"
	ChangeMoved          ChangeKind = "moved"
	ChangeMovedOut       ChangeKind = "moved_out"
	ChangeContentChanged ChangeKind = "content_changed"
	ChangeRejected       ChangeKind = "rejected"
)

func (k ChangeKind) Valid() bool {
	return k == ChangeSame || k == ChangeRejected || k.Applied()
}

// Applied는 카드에 변화를 만든 판정인지 알려 준다. 같음(same)은 글이 말한 값을 덮지만 변화로 세지 않고, 거절(rejected)은 기록만 한다.
func (k ChangeKind) Applied() bool {
	switch k {
	case ChangeTimeChanged, ChangeConfirmed, ChangeTentative, ChangeCancelled, ChangeOff,
		ChangeAdded, ChangeMoved, ChangeMovedOut, ChangeContentChanged:
		return true
	case ChangeSame, ChangeRejected:
		return false
	default:
		return false
	}
}

const (
	MaxPostChars       = 4000
	MaxChangeItems     = 20
	MaxChangeTextChars = 500
)

const (
	maxChangeReasonChars = 40
	offTitleKo           = "휴방"
	streamTitleKo        = "방송"
)

// Post는 포스터 게시 뒤 멤버가 X에 올린 원글이다. Body가 판별·대응·근거 검증의 유일한 본문이다.
// Status와 MatchedPosterID·MatchedRowsAt은 PendingPosts가 읽은 값이고, RecordMatch는 지금 값이 이와 같을 때만 쓴다.
type Post struct {
	ID        int64
	PostedAt  time.Time
	Body      string
	Status    PostStatus
	CreatedAt time.Time
	// MatchedPosterID·MatchedRowsAt은 마지막으로 대응한 행 묶음이며, 대응한 적이 없으면 zero다.
	MatchedPosterID int64
	MatchedRowsAt   time.Time
}

// PostDecision의 Kind가 비면 판별 답을 저장하지 않는다(judge_failed).
type PostDecision struct {
	Status       PostStatus
	Reason       string
	ScheduleInfo float64
	Kind         string
}

// MatchResult의 Status는 PostMatched나 PostMatchFailed다.
type MatchResult struct {
	Status  PostStatus
	Reason  string
	Changes []Change
}

// Change는 글 하나의 한 항목에서 뽑은 일정 변경이다. 글이 말한 값만 담는다.
// 대상은 원래 행 번호 Slot이거나 앞선 added 변경 (TargetPostID, TargetItem)이고, 둘 다 없는 added는 새 칸이다.
type Change struct {
	PostID int64
	Item   int
	// PostedAt은 적용 순서를 정한다. 저장소가 읽을 때 글에서 채우고, admin은 기록 전 적용에 쓰려고 직접 채운다.
	PostedAt     time.Time
	RecordedAt   time.Time
	Kind         ChangeKind
	Reason       string
	Slot         int
	TargetPostID int64
	TargetItem   int
	// Day가 zero면 날짜를 바꾸지 않는다.
	Day time.Time
	// Start가 nil이면 시각을 바꾸지 않고 ""면 시각을 지운다.
	Start *string
	// Status가 ""면 상태를 바꾸지 않는다.
	Status RowStatus
	Text   string
	// TitleKo가 ""면 한국어 다섯 칸(TitleKo, WithKo, PlaceKo, TagsKo, NoteKo)을 바꾸지 않는다.
	TitleKo  string
	WithKo   []string
	PlaceKo  string
	TagsKo   []string
	NoteKo   string
	Terms    []Term
	DoubtKo  string
	Warnings []string
}

// Slot은 원래 행에 변경을 적용한 현재 일정 한 칸이다. 더한 칸은 Ordinal이 0이고 AddedPostID·AddedItem이 그 칸을 만든 변경이다.
type Slot struct {
	Row

	// Number는 대응 입력의 줄 번호로, 원래 행 뒤에 더한 칸을 적용 순서로 이은 위치 + 1이다.
	Number      int
	AddedPostID int64
	AddedItem   int
	// OrigDay·OrigStart는 포스터 값이다. 더한 칸은 비어 있다.
	OrigDay   time.Time
	OrigStart string
	// MovedTo는 주 밖으로 옮긴 새 날짜다. 그 칸은 원래 날짜에 취소로 남는다.
	MovedTo time.Time
	// TimeChanged는 원래 행의 날짜·시각이 NormalizeDayTime으로 견줘 포스터 값과 다른지다.
	TimeChanged bool
	Steps       []Step
}

// Step은 칸에 반영한 변경 하나와 반영 직전의 날짜·시각이다.
type Step struct {
	Change

	PrevDay   time.Time
	PrevStart string
}

// PostURL은 출처 글의 주소다.
func PostURL(postID int64) string {
	return "https://x.com/i/status/" + strconv.FormatInt(postID, 10)
}

// NormalizeDayTime은 24시 이상 표기를 다음 날 시각으로 바꾸고 시를 두 자리로 맞춘다. 시각이 비었거나 형식이 아니면 그대로 돌려준다.
func NormalizeDayTime(day time.Time, start string) (time.Time, string) {
	if !rowTimePattern.MatchString(start) {
		return day, start
	}

	hour, minute, _ := strings.Cut(start, ":")

	h, err := strconv.Atoi(hour)
	if err != nil {
		return day, start
	}

	return day.AddDate(0, 0, h/24), fmt.Sprintf("%02d:%s", h%24, minute)
}

func sameDayTime(dayA time.Time, startA string, dayB time.Time, startB string) bool {
	dayA, startA = NormalizeDayTime(dayA, startA)
	dayB, startB = NormalizeDayTime(dayB, startB)

	return dayA.Equal(dayB) && startA == startB
}

// ApplyChanges는 ordinal 순 행에 변경을 (PostedAt, PostID, Item) 순서로 적용한 현재 일정을 돌려준다. 입력은 바꾸지 않는다.
// 같음은 글이 말한 날짜·시각·상태만 덮고 한국어 칸과 Steps는 그대로 둔다. 거절과 대상 칸을 찾지 못한 변경은 건너뛴다.
// 결과는 원래 행 뒤에 더한 칸을 적용 순서로 잇는다.
func ApplyChanges(rows []Row, changes []Change) []Slot {
	slots := make([]Slot, len(rows), len(rows)+len(changes))
	for i := range rows {
		slots[i] = Slot{Row: rows[i], OrigDay: rows[i].Day, OrigStart: rows[i].Start}
	}

	ordered := slices.Clone(changes)
	slices.SortStableFunc(ordered, func(a, b Change) int {
		return cmp.Or(a.PostedAt.Compare(b.PostedAt), cmp.Compare(a.PostID, b.PostID), cmp.Compare(a.Item, b.Item))
	})

	for i := range ordered {
		change := &ordered[i]

		switch {
		case change.Kind == ChangeSame:
			if j := slices.IndexFunc(slots, change.targets); j >= 0 {
				slots[j].setValues(change)
			}
		case !change.Kind.Applied():
		case change.addsSlot():
			slots = append(slots, newSlot(change))
		default:
			if j := slices.IndexFunc(slots, change.targets); j >= 0 {
				slots[j].apply(change)
			}
		}
	}

	for i := range slots {
		slot := &slots[i]

		slot.Number = i + 1
		slot.TimeChanged = slot.AddedPostID == 0 && !sameDayTime(slot.Day, slot.Start, slot.OrigDay, slot.OrigStart)
	}

	return slots
}

func (c *Change) targets(slot Slot) bool {
	if c.Slot > 0 {
		return slot.AddedPostID == 0 && slot.Ordinal == c.Slot
	}

	return c.TargetPostID > 0 && slot.AddedPostID == c.TargetPostID && slot.AddedItem == c.TargetItem
}

func (c *Change) addsSlot() bool {
	return c.Kind == ChangeAdded && c.Slot == 0 && c.TargetPostID == 0
}

func newSlot(change *Change) Slot {
	slot := Slot{
		Day: change.Day, Status: change.Status, SourceText: change.Text,
		AddedPostID: change.PostID, AddedItem: change.Item, Steps: []Step{{Change: *change}},
	}

	if change.Start != nil {
		slot.Start = *change.Start
	}

	slot.setKorean(change)

	return slot
}

func (s *Slot) apply(change *Change) {
	s.Steps = append(s.Steps, Step{Change: *change, PrevDay: s.Day, PrevStart: s.Start})
	s.setKorean(change)

	if change.Kind == ChangeMovedOut {
		s.Status = RowCancelled
		s.MovedTo = change.Day

		return
	}

	s.setValues(change)
}

func (s *Slot) setValues(change *Change) {
	if !change.Day.IsZero() {
		s.Day = change.Day
		s.MovedTo = time.Time{}
	}

	if change.Status != "" {
		s.Status = change.Status
		s.MovedTo = time.Time{}
	}

	if change.Start != nil && *change.Start != s.Start {
		s.Start = *change.Start
		s.End = ""
	}
}

func (s *Slot) setKorean(change *Change) {
	switch {
	case change.TitleKo != "":
		s.TitleKo, s.WithKo, s.PlaceKo, s.TagsKo, s.NoteKo = change.TitleKo, change.WithKo, change.PlaceKo, change.TagsKo, change.NoteKo
	case change.Kind == ChangeOff:
		s.TitleKo, s.WithKo, s.PlaceKo, s.TagsKo, s.NoteKo = offTitleKo, nil, "", nil, ""
	case change.Kind == ChangeAdded:
		s.TitleKo, s.WithKo, s.PlaceKo, s.TagsKo, s.NoteKo = streamTitleKo, nil, "", nil, ""
	}
}

// ValidateChanges는 migration 020의 CHECK와 같은 조건을 DB에 가기 전에 검사한다. 거절 행은 사유와 칸 번호만 가질 수 있고,
// 잘못된 UTF-8과 NUL 문자, 네 자리 연도가 아닌 날짜, 같은 글의 같은 항목 번호도 거부한다.
func ValidateChanges(changes []Change) error {
	type key struct {
		postID int64
		item   int
	}

	seen := make(map[key]bool, len(changes))

	for i := range changes {
		change := &changes[i]
		if err := change.validate(); err != nil {
			return fmt.Errorf("validate schedule poster changes: change %d: %w", i+1, err)
		}

		k := key{change.PostID, change.Item}
		if seen[k] {
			return fmt.Errorf("validate schedule poster changes: change %d: duplicate item %d of post %d", i+1, change.Item, change.PostID)
		}

		seen[k] = true
	}

	return nil
}

func (c *Change) validate() error {
	for _, check := range [...]func() error{c.validateIdentity, c.validateTarget, c.validateShape, c.validateFormat} {
		if err := check(); err != nil {
			return err
		}
	}

	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"source_text", c.Text, MaxChangeTextChars},
		{"title_ko", c.TitleKo, maxRowTextChars},
		{"place_ko", c.PlaceKo, maxRowTextChars},
		{"note_ko", c.NoteKo, maxRowTextChars},
		{"doubt_ko", c.DoubtKo, maxRowTextChars},
	} {
		if n := utf8.RuneCountInString(field.value); n > field.max {
			return fmt.Errorf("%s must be at most %d characters, got %d", field.name, field.max, n)
		}
	}

	for _, list := range [...][]string{c.WithKo, c.TagsKo, c.Warnings} {
		if len(list) > maxRowListItems {
			return fmt.Errorf("list must have at most %d items, got %d", maxRowListItems, len(list))
		}
	}

	if !c.storable() {
		return errors.New("text must be valid UTF-8 without NUL")
	}

	return nil
}

func (c *Change) validateIdentity() error {
	switch {
	case c.PostID <= 0:
		return fmt.Errorf("post id must be positive, got %d", c.PostID)
	case c.Item < 1 || c.Item > MaxChangeItems:
		return fmt.Errorf("item must be 1..%d, got %d", MaxChangeItems, c.Item)
	case !c.Kind.Valid():
		return fmt.Errorf("invalid kind %q", c.Kind)
	case (c.Kind == ChangeRejected) != (c.Reason != ""):
		return errors.New("reason must be set exactly for rejected changes")
	case utf8.RuneCountInString(c.Reason) > maxChangeReasonChars:
		return fmt.Errorf("reason must be at most %d characters", maxChangeReasonChars)
	}

	return nil
}

func (c *Change) validateTarget() error {
	switch {
	case c.Slot < 0 || c.Slot > MaxRows:
		return fmt.Errorf("slot must be 1..%d, got %d", MaxRows, c.Slot)
	case c.TargetPostID < 0 || (c.TargetPostID == 0) != (c.TargetItem == 0):
		return errors.New("target post and item must be set together")
	case c.TargetItem < 0 || c.TargetItem > MaxChangeItems:
		return fmt.Errorf("target item must be 1..%d, got %d", MaxChangeItems, c.TargetItem)
	case c.Slot != 0 && c.TargetPostID != 0:
		return errors.New("slot and target post cannot both be set")
	case c.Kind.Applied() && c.Kind != ChangeAdded && c.Slot == 0 && c.TargetPostID == 0:
		return fmt.Errorf("%s change needs a target", c.Kind)
	}

	return nil
}

func (c *Change) validateShape() error {
	switch {
	case c.Kind == ChangeRejected && (c.TargetPostID != 0 || c.Text != "" || c.translated() || c.carriesValues()):
		return errors.New("rejected change can carry only reason and slot")
	case c.Kind == ChangeSame && c.korean():
		return errors.New("same change cannot carry korean fields")
	case (c.Kind == ChangeMoved || c.Kind == ChangeMovedOut) && c.Day.IsZero():
		return fmt.Errorf("%s change needs a day", c.Kind)
	case c.addsSlot() && (c.Day.IsZero() || c.Start == nil || c.Status == ""):
		return errors.New("new slot needs day, time and status")
	}

	return nil
}

func (c *Change) validateFormat() error {
	switch {
	case !c.Day.IsZero() && (c.Day.Year() < 1 || c.Day.Year() > 9999):
		return fmt.Errorf("day %q must be a date in years 1..9999", c.Day.Format(time.DateOnly))
	case c.Start != nil && *c.Start != "" && !rowTimePattern.MatchString(*c.Start):
		return errors.New("time is not H:MM or HH:MM up to 29:59")
	case c.Status != "" && !c.Status.Valid():
		return fmt.Errorf("invalid status %q", c.Status)
	}

	return nil
}

func (c *Change) carriesValues() bool {
	return !c.Day.IsZero() || c.Start != nil || c.Status != ""
}

func (c *Change) korean() bool {
	return c.TitleKo != "" || len(c.WithKo) > 0 || c.PlaceKo != "" || len(c.TagsKo) > 0 || c.NoteKo != ""
}

func (c *Change) translated() bool {
	return c.korean() || len(c.Terms) > 0 || c.DoubtKo != "" || len(c.Warnings) > 0
}

func (c *Change) storable() bool {
	texts := []string{c.Reason, c.Text, c.TitleKo, c.PlaceKo, c.NoteKo, c.DoubtKo}
	if c.Start != nil {
		texts = append(texts, *c.Start)
	}

	for _, list := range [...][]string{c.WithKo, c.TagsKo, c.Warnings} {
		texts = append(texts, list...)
	}

	for _, term := range c.Terms {
		texts = append(texts, term.Source, term.Ko, term.Basis)
	}

	for _, text := range texts {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return false
		}
	}

	return true
}
