package app

import workerapp "github.com/kapu/hololive-alarm-worker/internal/app/workerapp"

type AlarmWorkerRuntime = workerapp.AlarmWorkerRuntime

var BuildAlarmWorkerRuntime = workerapp.BuildAlarmWorkerRuntime
var BuildAlarmWorkerConfigSubscriber = workerapp.BuildAlarmWorkerConfigSubscriber
