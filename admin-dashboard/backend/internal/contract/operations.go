package contract

import "slices"

// Operation은 정본에서 생성한 HTTP operation과 필수 접근 분류입니다.
type Operation struct {
	Method   string
	Path     string
	ID       string
	Access   string
	Mutation bool
}

// Operations는 호출자가 정본의 접근 목록을 변경하지 못하도록 복사본을 반환합니다.
func Operations() []Operation {
	return slices.Clone(operations)
}
