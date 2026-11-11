package domain

type AlarmDispatchSourceKind string

const (
	AlarmDispatchSourceKindCelebration AlarmDispatchSourceKind = "celebration"
	// AlarmDispatchSourceKindXSpace는 X 스페이스 시작 알림을 기존 LIVE 구독으로 전달한다.
	AlarmDispatchSourceKindXSpace AlarmDispatchSourceKind = "x_space"
)
