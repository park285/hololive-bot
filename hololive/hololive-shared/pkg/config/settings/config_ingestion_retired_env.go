package settings

import (
	"fmt"
	"os"
)

// YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT은 community·shorts 대전환의 관측 창을 표시하려던 시각이었지만
// IngestionConfig.CommunityShortsBigBangCutoverAt을 읽는 코드가 없었다. 감사 T11(stack-audit 2026-09-26,
// holo-community-shorts-bigbang-cutover-env, DEC-20260926-hololive-legacy-env-config-retirement)에서 필드와 파서를
// 지웠다. 값을 읽지 않고 존재만으로 거절한다. 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T11).
//
// 배포 선행 조건이자 제거 조건: settings.LoadConfig를 쓰는 runtime(hololive-api bot·admin plane, alarm-worker)의 env
// 원천인 중앙 /etc/stack-secrets/hololive-bot/bot.env와 alarm-worker.env, 중앙 compose.env, 그 stack-secrets master 사본,
// 그리고 실행 중 프로세스 env에 키가 0건임을 hololive-bot-ops로 확인한다. T18(2026-09-26)에서 키가 남은 곳은 모든
// youtube-collector.env(collector는 이 키를 읽지 않는다)였고 같은 배포 선행 작업에서 함께 지운다. 이 가드가 든 release가
// 중앙 호스트에 배포된 뒤 이 파일, 테스트, rejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredIngestionEnvKeys = []string{
	"YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT",
}

func rejectRetiredIngestionEnv() error {
	for _, key := range retiredIngestionEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: the value had no consumer; remove it from the runtime env", key)
		}
	}

	return nil
}
