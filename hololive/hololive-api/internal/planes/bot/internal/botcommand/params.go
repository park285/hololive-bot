package botcommand

import contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"

type Params interface {
	commandParams()
}

type NoParams struct{}

type LiveParams struct {
	Member string
}

type UpcomingParams struct {
	Member string
	Limit  int
	All    bool
}

type ScheduleParams struct {
	Member     string
	Days       int
	RawCommand string
}

type SchedulePosterParams struct {
	Member string
}

type BroadcastHistoryParams struct {
	Member string
	Type   string
	Topic  string
	Days   int
	Limit  int
}

type BroadcastTimeParams struct {
	Member string
	Days   int
}

type BroadcastThumbnailParams struct {
	VideoID string
}

type AlarmParams struct {
	Action string
	Member string
	Type   string
}

type EventGradeParams struct {
	Score  int64
	Region contract.GradeRegion
}

type MemberInfoParams struct {
	Query string
}

type CalendarParams struct {
	Month       int
	MonthOffset int
}

func (NoParams) commandParams()                 {}
func (LiveParams) commandParams()               {}
func (UpcomingParams) commandParams()           {}
func (ScheduleParams) commandParams()           {}
func (SchedulePosterParams) commandParams()     {}
func (BroadcastHistoryParams) commandParams()   {}
func (BroadcastTimeParams) commandParams()      {}
func (BroadcastThumbnailParams) commandParams() {}
func (AlarmParams) commandParams()              {}
func (EventGradeParams) commandParams()         {}
func (MemberInfoParams) commandParams()         {}
func (CalendarParams) commandParams()           {}
