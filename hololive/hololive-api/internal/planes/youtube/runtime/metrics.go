package runtime

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var youtubeClaimLostTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "hololive_youtube_plane_claim_lost_total",
	Help: "Total YouTube plane observations skipped because their claim was no longer owned.",
})
