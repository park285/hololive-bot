package settings

import (
	"fmt"
	"os"
)

// IRIS_WEBHOOK_REQUIRE_HMAC는 Iris webhook HMAC 검증을 끌 수 있던 설정이다. HMAC이 필수가 된 뒤에는 true 외 값을
// 거절하는 값 가드(도입 c33b66d60)만 남아 있었고 Webhook.RequireHMAC 필드를 읽는 production 코드는 없었다. 계획 T11 C8
// (stack-audit 2026-09-26)에서 compose(docker-compose.prod.yml x-iris-env)와 .env.example의 주입, 필드와 값 가드를
// 지웠다. 키가 남아 있으면 HMAC을 설정으로 끌 수 있다고 오해하게 하므로 값을 읽지 않고 존재만으로(빈 값·true 포함)
// 거절한다. 가드 도입 리비전: stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T11,
// holo-iris-webhook-require-hmac-flag·holo-retired-guard-iris-webhook-require-hmac).
//
// 배포 선행 조건이자 제거 조건: settings.LoadConfig를 쓰는 runtime(hololive-api bot·admin plane, alarm-worker)의
// 컨테이너 env 원천인 중앙 compose.env(이 release 이전 compose가 x-iris-env로 주입), bot.env(HOLOLIVE_API_ENV_FILE)와
// alarm-worker.env(HOLOLIVE_ALARM_WORKER_ENV_FILE), 그 stack-secrets master 사본, 그리고 실행 중 프로세스 env에
// IRIS_WEBHOOK_REQUIRE_HMAC 키가 0건이어야 한다. T18(2026-09-26)은 중앙 compose.env에 이 키가 있음을 확인했으므로
// hololive-bot-ops가 그 키를 먼저 지우고 0건을 확인한 뒤에 이 가드가 든 release를 배포한다. 배포된 뒤 한 release가 지나면
// 이 파일, 테스트, rejectRetiredRuntimeEnv(config_build.go)의 호출을 함께 삭제한다. 재검토 기한: remove_after = "2026-12-31".
var retiredWebhookEnvKeys = []string{
	"IRIS_WEBHOOK_REQUIRE_HMAC",
}

func rejectRetiredWebhookEnv() error {
	for _, key := range retiredWebhookEnvKeys {
		if _, found := os.LookupEnv(key); found {
			return fmt.Errorf("%s is retired: Iris webhook HMAC is always required; remove it from the runtime env", key)
		}
	}

	return nil
}
