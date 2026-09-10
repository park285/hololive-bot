// contract-fixtures는 수동 Go DTO를 생성 validator와 대조할 비밀 없는 fixture를 출력합니다.
package main

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"strconv"

	"github.com/kapu/admin-dashboard/internal/contract"
)

func main() {
	fixtures := map[string]any{
		"ErrorResponse": contract.ErrorResponse{Code: "UPSTREAM_AUTH_FAILED", Error: "내부 서비스 인증을 확인하지 못했습니다.", RequestID: "fixture-request"},
		"AdminMetadata": contract.AdminMetadata{ClientGeneration: contract.Generation},
		"Member": contract.Member{
			ID: strconv.FormatInt(9007199254740993, 10), ChannelID: "UC-fixture", Name: "한글",
			Aliases: contract.Aliases{KO: []string{}, JA: []string{}},
		},
	}

	if err := jsonv2.MarshalWrite(os.Stdout, fixtures, jsonv2.Deterministic(true)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "marshal contract fixtures: %v\n", err)

		os.Exit(1)
	}
}
