package sqlassets

import (
	"fmt"
	"io/fs"
	"strings"
)

// MustReader는 directory 아래 임베드 SQL을 읽는 패키지 전용 함수를 반환한다.
// 파일이 없거나 잘못되었거나 비어 있으면 기존 mustSQL 계약대로 해당 경로를 포함해 panic한다.
//
// 반환 문자열은 자산 바이트 그대로다(TrimSpace 없음). 여러 호출자가
// `mustSQL(...) + fragment` 형태로 조각을 이어 붙이면서 자산 끝의 공백을
// 토큰 구분자로 쓰고 있어, 여기서 trim하면 `WHERE` + `id IN (...)`가
// `WHEREid IN (...)`로 붙는다.
func MustReader(assets fs.FS, directory string) func(string) string {
	directory = strings.TrimSuffix(directory, "/")
	if !fs.ValidPath(directory) || directory == "." {
		panic(fmt.Errorf("invalid embedded SQL directory %q", directory))
	}

	return func(name string) string {
		queryPath := directory + "/" + name
		if !fs.ValidPath(queryPath) {
			panic(fmt.Errorf("invalid embedded SQL path %q", queryPath))
		}

		query, err := fs.ReadFile(assets, queryPath)
		if err != nil {
			panic(fmt.Errorf("read embedded SQL %q: %w", queryPath, err))
		}

		if strings.TrimSpace(string(query)) == "" {
			panic(fmt.Errorf("empty embedded SQL %q", queryPath))
		}

		return string(query)
	}
}
