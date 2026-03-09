// Package docker: Docker 컨테이너 관리
package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Container: Docker 컨테이너 상태 및 메타데이터
type Container struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	State     string    `json:"state"`
	Status    string    `json:"status"`
	Health    string    `json:"health"`
	Managed   bool      `json:"managed"`
	Paused    bool      `json:"paused"`
	StartedAt time.Time `json:"startedAt"`
}

// Service: Docker 서비스
type Service struct {
	client         *client.Client
	logger         *slog.Logger
	projectName    string
	managedFilters []string
	excludeFilters []string // 관리 대상에서 제외할 패턴
}

// NewService: Docker 서비스 생성
func NewService(logger *slog.Logger, projectName string) (*Service, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	return &Service{
		client:      cli,
		logger:      logger.With(slog.String("component", "docker")),
		projectName: projectName,
		managedFilters: []string{
			"hololive",
			"valkey",
			"postgres",
			"deunhealth",
		},
		excludeFilters: []string{
			"-init", // init container 제외
		},
	}, nil
}

// Available: Docker 데몬 가용성 확인
func (s *Service) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := s.client.Ping(ctx)
	return err == nil
}

// ListContainers: 관리 대상 컨테이너 목록 조회
func (s *Service) ListContainers(ctx context.Context) ([]Container, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	containers, err := s.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	result := make([]Container, 0, len(containers))
	for i := range containers {
		c := &containers[i]
		name := strings.TrimPrefix(c.Names[0], "/")

		if !s.isManaged(name) {
			continue
		}

		health := "none"
		if c.State == "running" && strings.Contains(c.Status, "(") {
			start := strings.Index(c.Status, "(")
			end := strings.Index(c.Status, ")")
			if start != -1 && end != -1 && end > start {
				health = c.Status[start+1 : end]
			}
		}

		result = append(result, Container{
			ID:      c.ID[:12],
			Name:    name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Health:  health,
			Managed: s.isManaged(name),
			Paused:  c.State == "paused",
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// RestartContainer: 컨테이너 재시작
func (s *Service) RestartContainer(ctx context.Context, name string) error {
	s.logger.Info("restarting container", slog.String("container", name))

	timeout := 30
	stopOptions := container.StopOptions{Timeout: &timeout}

	if err := s.client.ContainerRestart(ctx, name, stopOptions); err != nil {
		s.logger.Error("failed to restart container",
			slog.String("container", name),
			slog.String("error", err.Error()))
		return fmt.Errorf("restart container %s: %w", name, err)
	}

	s.logger.Info("container restarted", slog.String("container", name))
	return nil
}

// StopContainer: 컨테이너 중지
func (s *Service) StopContainer(ctx context.Context, name string) error {
	s.logger.Info("stopping container", slog.String("container", name))

	timeout := 30
	stopOptions := container.StopOptions{Timeout: &timeout}

	if err := s.client.ContainerStop(ctx, name, stopOptions); err != nil {
		s.logger.Error("failed to stop container",
			slog.String("container", name),
			slog.String("error", err.Error()))
		return fmt.Errorf("stop container %s: %w", name, err)
	}

	s.logger.Info("container stopped", slog.String("container", name))
	return nil
}

// StartContainer: 컨테이너 시작
func (s *Service) StartContainer(ctx context.Context, name string) error {
	s.logger.Info("starting container", slog.String("container", name))

	if err := s.client.ContainerStart(ctx, name, container.StartOptions{}); err != nil {
		s.logger.Error("failed to start container",
			slog.String("container", name),
			slog.String("error", err.Error()))
		return fmt.Errorf("start container %s: %w", name, err)
	}

	s.logger.Info("container started", slog.String("container", name))
	return nil
}

// GetLogStream: 컨테이너 로그 스트림
func (s *Service) GetLogStream(ctx context.Context, name string) (io.ReadCloser, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "100",
		Timestamps: true,
	}

	reader, err := s.client.ContainerLogs(ctx, name, options)
	if err != nil {
		return nil, fmt.Errorf("container %s logs: %w", name, err)
	}
	return reader, nil
}

// IsManaged: 관리 대상 여부 확인
func (s *Service) IsManaged(name string) bool {
	if s == nil {
		return false
	}
	return s.isManaged(name)
}

func (s *Service) isManaged(name string) bool {
	// exclude 패턴 먼저 확인
	for _, pattern := range s.excludeFilters {
		if strings.Contains(name, pattern) {
			return false
		}
	}
	// managed 패턴 확인
	for _, filter := range s.managedFilters {
		if strings.Contains(name, filter) {
			return true
		}
	}
	return false
}

// Close: Docker 클라이언트 종료
func (s *Service) Close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("close docker client: %w", err)
	}
	return nil
}
