package tracking

import observation "github.com/kapu/hololive-shared/pkg/service/youtube/tracking/internal/observation"

type AlarmSentMark = observation.AlarmSentMark

type PgxRepository = observation.PgxRepository

type ReadRepository = observation.ReadRepository

type Repository = observation.Repository

type WriteRepository = observation.WriteRepository

var NewRepository = observation.NewRepository
