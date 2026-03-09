// Package docker: Docker 컨테이너 관리
package docker

import (
	"context"
	"io"
)

// DockerProvider: docker.Service의 메서드들을 인터페이스로 추출
// - 테스트에서 mock 주입 가능하도록 함
// - 기존 *Service는 Go의 암묵적 인터페이스로 자동 만족
type DockerProvider interface {
	Available(ctx context.Context) bool
	ListContainers(ctx context.Context) ([]Container, error)
	RestartContainer(ctx context.Context, name string) error
	StopContainer(ctx context.Context, name string) error
	StartContainer(ctx context.Context, name string) error
	GetLogStream(ctx context.Context, name string) (io.ReadCloser, error)
	IsManaged(name string) bool
	Close() error
}
