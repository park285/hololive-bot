package messaging

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

var eventScore = regexp.MustCompile(`^(\d+|\d{1,3}(,\d{3})+)$`)

func parseEventGrade(command string, args []string, _ string) (*ParsedCommand, bool) {
	if command != "이벤트등급" {
		return nil, false
	}

	cmd := &ParsedCommand{Type: botcommand.CommandEventGrade, Params: botcommand.EventGradeParams{}}

	if len(args) < 1 || len(args) > 2 || len(args[0]) > 25 || !eventScore.MatchString(args[0]) {
		return cmd, true
	}

	score, err := strconv.ParseInt(strings.ReplaceAll(args[0], ",", ""), 10, 64)
	if err != nil || score <= 0 {
		return cmd, true
	}

	region := contract.GradeRegion("jp")

	if len(args) == 2 {
		switch strings.ToLower(args[1]) {
		case "jp", "일본":
		case "us", "미국":
			region = "us"
		case "global", "as", "글로벌", "아시아":
			region = "as"
		default:
			return cmd, true
		}
	}

	cmd.Params = botcommand.EventGradeParams{Score: score, Region: region}

	return cmd, true
}
