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

type Post struct {
	ID              int64
	PostedAt        time.Time
	Body            string
	Status          PostStatus
	CreatedAt       time.Time
	MatchedPosterID int64
	MatchedRowsAt   time.Time
}

type PostDecision struct {
	Status       PostStatus
	Reason       string
	ScheduleInfo float64
	Kind         string
}

type MatchResult struct {
	Status  PostStatus
	Reason  string
	Changes []Change
}

type Change struct {
	PostID       int64
	Item         int
	PostedAt     time.Time
	RecordedAt   time.Time
	Kind         ChangeKind
	Reason       string
	Slot         int
	TargetPostID int64
	TargetItem   int
	Day          time.Time
	Start        *string
	Status       RowStatus
	Text         string
	TitleKo      string
	WithKo       []string
	PlaceKo      string
	TagsKo       []string
	NoteKo       string
	Terms        []Term
	DoubtKo      string
	Warnings     []string
	EditedAt     time.Time
}

type Slot struct {
	Row

	Number      int
	AddedPostID int64
	AddedItem   int
	OrigDay     time.Time
	OrigStart   string
	MovedTo     time.Time
	TimeChanged bool
	Steps       []Step
}

type Step struct {
	Change

	PrevDay   time.Time
	PrevStart string
}

func PostURL(postID int64) string {
	return "https://x.com/i/status/" + strconv.FormatInt(postID, 10)
}

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

func (c *Change) setsKorean() bool {
	return c.TitleKo != "" || c.Kind == ChangeOff || c.Kind == ChangeAdded
}

func (s *Slot) setKorean(change *Change) {
	if !change.setsKorean() {
		return
	}

	s.EditedAt = change.EditedAt

	switch {
	case change.TitleKo != "":
		s.TitleKo, s.WithKo, s.PlaceKo, s.TagsKo, s.NoteKo = change.TitleKo, change.WithKo, change.PlaceKo, change.TagsKo, change.NoteKo
	case change.Kind == ChangeOff:
		s.TitleKo, s.WithKo, s.PlaceKo, s.TagsKo, s.NoteKo = offTitleKo, nil, "", nil, ""
	default:
		s.TitleKo, s.WithKo, s.PlaceKo, s.TagsKo, s.NoteKo = streamTitleKo, nil, "", nil, ""
	}
}

func (s *Slot) koreanStep() (*Change, bool) {
	for i := range slices.Backward(s.Steps) {
		if change := &s.Steps[i].Change; change.setsKorean() {
			return change, true
		}
	}

	return nil, false
}

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
