package scheduleposter

import (
	"cmp"
	_ "embed"
	"strconv"
	"strings"
	"time"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

//go:embed prompts/match.txt
var matchPromptTemplate string

const matchClockLayout = "2006-01-02 15:04"

type matchRow struct {
	Number  int
	Day     time.Time
	Start   string
	TZ      string
	Status  posterstore.RowStatus
	Text    string
	Updates []string
}

func matchRows(slots []posterstore.Slot) []matchRow {
	rows := make([]matchRow, len(slots))

	for i := range slots {
		slot := &slots[i]
		row := matchRow{
			Number: slot.Number, Day: slot.Day, Start: slot.Start, TZ: slot.TZ, Status: slot.Status, Text: slot.SourceText,
			Updates: make([]string, 0, len(slot.Steps)),
		}

		for k := range slot.Steps {
			if update, ok := stepUpdate(&slot.Steps[k]); ok {
				row.Updates = append(row.Updates, update)
			}
		}

		rows[i] = row
	}

	return rows
}

func stepUpdate(step *posterstore.Step) (string, bool) {
	start := optional(step.Start)

	var memo string

	switch step.Kind {
	case posterstore.ChangeTimeChanged:
		memo = "start " + cmp.Or(step.PrevStart, "none") + " -> " + start
	case posterstore.ChangeConfirmed:
		memo = "confirmed" + prefixed(" at ", start)
	case posterstore.ChangeTentative:
		memo = "time undecided"

		if start != "" {
			memo += " (around " + start + ")"
		}
	case posterstore.ChangeCancelled:
		memo = string(posterstore.ChangeCancelled)
	case posterstore.ChangeOff:
		memo = "day off"
	case posterstore.ChangeMoved, posterstore.ChangeMovedOut:
		memo = "moved from " + step.PrevDay.Format(time.DateOnly) + " to " + step.Day.Format(time.DateOnly) + prefixed(" ", start)
	case posterstore.ChangeAdded:
		if step.Slot == 0 && step.TargetPostID == 0 {
			return "added by post " + step.PostedAt.In(jst).Format(matchClockLayout), true
		}

		memo = "stream announced on this day"
	case posterstore.ChangeContentChanged:
	case posterstore.ChangeSame, posterstore.ChangeRejected:
		return "", false
	default:
		return "", false
	}

	if step.Text != "" {
		if memo != "" {
			memo += "; "
		}

		memo += `"` + markerSafe(step.Text) + `"`
	}

	return memo, true
}

func prefixed(prefix, value string) string {
	if value == "" {
		return ""
	}

	return prefix + value
}

func (r *matchRow) line() string {
	var line strings.Builder

	line.WriteString(strconv.Itoa(r.Number) + ". " + r.Day.Format(time.DateOnly))

	if r.Start != "" {
		line.WriteString(" " + r.Start + prefixed(" ", r.TZ))
	}

	line.WriteString(" [" + string(r.Status) + "] " + markerSafe(r.Text))

	for _, update := range r.Updates {
		line.WriteString(" [update: " + update + "]")
	}

	return line.String()
}

func buildMatchPrompt(post posterstore.Post, weekStart, weekEnd time.Time, rows []matchRow) string {
	var prompt strings.Builder

	prompt.WriteString(matchPromptTemplate)
	prompt.WriteString("Post timestamp: " + post.PostedAt.In(jst).Format(matchClockLayout) + " JST\n")
	prompt.WriteString("Current week: " + weekStart.Format(time.DateOnly) + " to " + weekEnd.Format(time.DateOnly) + "\n")
	prompt.WriteString("Current schedule rows:\n")

	for i := range rows {
		if i > 0 {
			prompt.WriteString("\n")
		}

		prompt.WriteString(rows[i].line())
	}

	prompt.WriteString("\nPost text:\n" + post.Body)

	return prompt.String()
}
