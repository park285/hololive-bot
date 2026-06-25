module github.com/kapu/hololive-api

go 1.26.2

toolchain go1.26.4

require (
	github.com/kapu/hololive-admin-api v0.0.0
	github.com/kapu/hololive-kakao-bot-go v0.0.0
	github.com/kapu/hololive-llm-sched v0.0.0
	github.com/kapu/hololive-shared v0.0.0
	github.com/park285/shared-go v1.19.0
)

replace github.com/kapu/hololive-admin-api => ../hololive-admin-api

replace github.com/kapu/hololive-kakao-bot-go => ../hololive-kakao-bot-go

replace github.com/kapu/hololive-llm-sched => ../hololive-llm-sched

replace github.com/kapu/hololive-shared => ../hololive-shared
