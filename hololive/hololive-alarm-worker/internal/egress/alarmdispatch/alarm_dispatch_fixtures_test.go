package alarmdispatch

import "time"

const (
	testAlarmRoomID          = "room-1"
	testAlarmGoldenRoomID    = "room-golden"
	testAlarmChannelID       = "UCtest"
	testCelebrationChannelID = "UC_test"
	testAlarmMemberName      = "Member"
	testAlarmClaimKey        = "alarm:dispatch:claim:room-1:stream-1"
	testIrisPostOp           = "post"
	testIrisReplyPath        = "/reply"
	testRetryClientRequestID = "hololive-alarm:0123456789abcdef0123456789abcdef"
)

// DB 시험의 격리 기준은 lease에서 계산하지 않는 고정값이며 기본 lease(60s)보다 길다.
const testAlarmQuarantineThreshold = 5 * time.Minute
