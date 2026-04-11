package outbox

import (
	"time"

	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

var sentAtNow = time.Now

func canonicalSentAtNow() time.Time {
	return yttimestamp.Normalize(sentAtNow())
}
