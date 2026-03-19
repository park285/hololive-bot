package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/settlement-go/pkg/settlement"
)

type messageFormatter struct{}

func (f *messageFormatter) formatStatus(cycle *settlement.Cycle, statuses []settlement.PaymentStatus) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "💰 %s 정산 현황\n", cycleLabel(cycle))
	fmt.Fprintf(&sb, "이용기간: %s | 갱신일: 매월 %d일\n", cycleRange(cycle), cycle.BillingAnchorDay)
	fmt.Fprintf(&sb, "인당 %s원\n", formatAmount(cycle.PerPerson))
	sb.WriteString("─────────────\n")

	paidCount := 0
	for _, s := range statuses {
		if s.PaidAt != nil {
			paidCount++
			fmt.Fprintf(&sb, "✅ %s (완료)\n", s.MemberNameSnapshot)
		} else {
			fmt.Fprintf(&sb, "⬜ %s\n", s.MemberNameSnapshot)
		}
	}

	sb.WriteString("─────────────\n")
	fmt.Fprintf(&sb, "진행: %d/%d명 완료", paidCount, len(statuses))
	return sb.String()
}

func (f *messageFormatter) formatAlarm(cycle *settlement.Cycle, unpaidNames []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🔔 %s 정산 알림\n", cycleLabel(cycle))
	fmt.Fprintf(&sb, "이용기간: %s | 갱신일: 매월 %d일\n", cycleRange(cycle), cycle.BillingAnchorDay)
	fmt.Fprintf(&sb, "인당 %s원\n\n", formatAmount(cycle.PerPerson))
	sb.WriteString("미완료:\n")
	for _, name := range unpaidNames {
		fmt.Fprintf(&sb, "  ⬜ %s\n", name)
	}
	sb.WriteString("\n!정산완료 로 납부를 체크해주세요.")
	return sb.String()
}

func cycleLabel(cycle *settlement.Cycle) string {
	kst, _ := time.LoadLocation("Asia/Seoul")
	start := cycle.PeriodStartAt.In(kst)
	return fmt.Sprintf("%d/%d 결제분", int(start.Month()), start.Day())
}

func cycleRange(cycle *settlement.Cycle) string {
	kst, _ := time.LoadLocation("Asia/Seoul")
	start := cycle.PeriodStartAt.In(kst)
	end := cycle.PeriodEndAt.In(kst).Add(-time.Second)
	return fmt.Sprintf("%d/%d ~ %d/%d", int(start.Month()), start.Day(), int(end.Month()), end.Day())
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
