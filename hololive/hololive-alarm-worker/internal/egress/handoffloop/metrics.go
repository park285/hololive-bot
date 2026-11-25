package handoffloop

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var handoffRoomNotJoinedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "alarm_handoff_room_not_joined_total",
		Help: "Total handoff checks settled because Iris reported that the bot account is not in the target room, by ledger.",
	},
	[]string{"ledger"},
)
