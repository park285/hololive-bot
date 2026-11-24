package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

type EventGradeRegistrar interface {
	RegisterEventGrade(context.Context, contract.EventGradeRequest) (contract.EventGradeResponse, error)
}

type EventGradeCommand struct {
	handlercore.BaseCommand

	register EventGradeRegistrar
}

func NewEventGradeCommand(deps *handlercore.Dependencies, register EventGradeRegistrar) *EventGradeCommand {
	return &EventGradeCommand{BaseCommand: handlercore.NewBaseCommand(deps), register: register}
}

func (*EventGradeCommand) Name() string { return string(botcommand.CommandEventGrade) }
func (*EventGradeCommand) Description() string {
	return "진행 중인 이벤트의 최종 등급 알림 등록"
}

func (c *EventGradeCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("event grade dependencies: %w", err)
	}

	return nil
}

func (c *EventGradeCommand) Execute(ctx context.Context, cmd *botcommand.CommandContext, params botcommand.EventGradeParams) error {
	text := c.responseText(ctx, cmd, params)
	if err := c.Deps().SendMessage(ctx, cmd.Room, text); err != nil {
		return fmt.Errorf("send event grade response: %w", err)
	}

	return nil
}

func (c *EventGradeCommand) responseText(ctx context.Context, cmd *botcommand.CommandContext, params botcommand.EventGradeParams) string {
	score, region := params.Score, params.Region

	if !region.Valid() || score <= 0 {
		return "사용법: !이벤트등급 <점수> [jp|us|global] (기본: jp)"
	}

	const (
		unavailable = "현재 이벤트 등급 알림을 등록할 수 없습니다."
		unconfirmed = "등록 결과를 확인하지 못했습니다. 같은 이벤트에서 다시 입력하면 점수만 안전하게 갱신됩니다."
	)

	if c.register == nil {
		return unavailable
	}

	response, err := c.register.RegisterEventGrade(ctx, contract.EventGradeRequest{RoomID: cmd.Room, UserID: cmd.UserID, Score: score, Region: region})
	if err != nil || !response.Valid(region) {
		return unconfirmed
	}

	if response.Status != "registered" {
		return unavailable
	}

	kind := "일반 마라톤"

	if response.EventType == "relay" {
		kind = "릴레이 마라톤"
	}

	regionName := strings.ToUpper(string(region))
	if region == "as" {
		regionName = "Global"
	}

	zone := time.FixedZone("KST", 9*60*60)

	return fmt.Sprintf("%s %s에 %d점을 등록했습니다. 종료까지 %s (종료 %s KST), 등급 공개 %s KST 이후 이 방으로 알려드립니다.", regionName, kind, score, max(time.Duration(0), time.Until(response.EndsAt)).Round(time.Second), response.EndsAt.In(zone).Format("01-02 15:04"), response.RevealsAt.In(zone).Format("01-02 15:04"))
}
