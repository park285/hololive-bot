package youtubedispatch

// 외부 test package(youtubedispatch_test)가 내부 동작을 쓰도록 테스트 빌드에서만 노출한다.

// WithTestDispatchConfigDefaults는 내부 테스트 기준값으로 빈 설정 필드를 채운다.
var WithTestDispatchConfigDefaults = withTestDispatchConfigDefaults
