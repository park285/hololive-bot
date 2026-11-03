package sqlassets

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

// MustReader는 directory 아래 임베드 SQL을 읽는 패키지 전용 함수를 반환한다.
// 파일이 없거나 잘못되었거나 비어 있으면 기존 mustSQL 계약대로 해당 경로를 포함해 panic한다.
//
// 반환 문자열은 자산 바이트 그대로다(TrimSpace 없음). 여러 호출자가
// `mustSQL(...) + fragment` 형태로 조각을 이어 붙이면서 자산 끝의 공백을
// 토큰 구분자로 쓰고 있어, 여기서 trim하면 `WHERE` + `id IN (...)`가
// `WHEREid IN (...)`로 붙는다.
//
// 반환 함수가 쓰이는 동안 assets는 embed.FS처럼 바뀌지 않아야 한다. 성공한 읽기만
// 이름별로 캐시하므로, 읽은 뒤 바뀐 자산 내용은 반영하지 않는다.
func MustReader(assets fs.FS, directory string) func(string) string {
	directory = strings.TrimSuffix(directory, "/")
	if !fs.ValidPath(directory) || directory == "." {
		panic(fmt.Errorf("invalid embedded SQL directory %q", directory))
	}

	// 렌더·발송마다 같은 자산을 다시 읽으며 생기던 ReadFile 복사를 없앤다.
	// 실패는 저장하지 않으므로 잘못된 이름은 호출할 때마다 같은 panic을 낸다.
	var cache sync.Map

	return func(name string) string {
		if cached, ok := cache.Load(name); ok {
			return cachedSQL(cached)
		}

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

		actual, _ := cache.LoadOrStore(name, string(query))

		return cachedSQL(actual)
	}
}

func cachedSQL(value any) string {
	query, ok := value.(string)
	if !ok {
		// 캐시에는 string만 저장하므로 여기에 오면 불변식 위반이다.
		panic(fmt.Errorf("embedded SQL cache stored %T, want string", value))
	}

	return query
}
