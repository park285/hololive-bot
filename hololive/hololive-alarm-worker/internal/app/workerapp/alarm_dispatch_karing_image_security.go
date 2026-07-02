package workerapp

import (
	"github.com/kapu/hololive-shared/pkg/net/imagehost"
)

func isAllowedKaringImageURL(raw string) bool {
	return imagehost.ThumbnailHosts.AllowsURL(raw)
}
