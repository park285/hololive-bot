package main

import (
	"fmt"
	"strings"

	"github.com/kapu/settlement-go/pkg/settlement"
)

type messageFormatter struct{}

// formatStatus: 정산 현황 포맷.
func (f *messageFormatter) formatStatus(cycle *settlement.Cycle, statuses []settlement.PaymentStatus) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "💰 %d년 %d월 정산 현황\n", cycle.Year, cycle.Month)
	fmt.Fprintf(&sb, "인당 %s원 | 납부 기한: %d일\n", formatAmount(cycle.PerPerson), cycle.DueDay)
	sb.WriteString("─────────────\n")

	paidCount := 0
	for _, s := range statuses {
		if s.PaidAt != nil {
			paidCount++
			fmt.Fprintf(&sb, "✅ %s (완료)\n", s.MemberName)
		} else {
			fmt.Fprintf(&sb, "⬜ %s\n", s.MemberName)
		}
	}
	sb.WriteString("─────────────\n")
	fmt.Fprintf(&sb, "진행: %d/%d명 완료", paidCount, len(statuses))
	return sb.String()
}

// formatAlarm: 스케줄러 미완료 알람 포맷.
func (f *messageFormatter) formatAlarm(unpaidNames []string, perPerson int, dueDay int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🔔 정산 알림 | 납부 기한: %d일\n", dueDay)
	fmt.Fprintf(&sb, "인당 %s원\n\n", formatAmount(perPerson))
	sb.WriteString("미완료:\n")
	for _, name := range unpaidNames {
		fmt.Fprintf(&sb, "  ⬜ %s\n", name)
	}
	sb.WriteString("\n!정산완료 로 납부를 체크해주세요.")
	return sb.String()
}

func formatAmount(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}
