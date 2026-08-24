package celebration

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func birthdayCelebrationOrdinal(birthday, debutDate *time.Time, currentYear int) int {
	if missingBirthdayOrdinalInput(birthday, debutDate, currentYear) {
		return 0
	}

	firstYear := debutDate.Year()
	if birthdayMonthDayBeforeDebut(birthday, debutDate) {
		firstYear++
	}

	if isLeapDay(birthday) {
		return leapDayBirthdayOrdinal(firstYear, currentYear)
	}

	if currentYear < firstYear {
		return 0
	}

	return currentYear - firstYear + 1
}

func missingBirthdayOrdinalInput(birthday, debutDate *time.Time, currentYear int) bool {
	return birthday == nil || debutDate == nil || currentYear <= 0
}

func birthdayMonthDayBeforeDebut(birthday, debutDate *time.Time) bool {
	if birthday.Month() != debutDate.Month() {
		return birthday.Month() < debutDate.Month()
	}

	return birthday.Day() < debutDate.Day()
}

func isLeapDay(date *time.Time) bool {
	return date.Month() == time.February && date.Day() == 29
}

func leapDayBirthdayOrdinal(firstYear, currentYear int) int {
	firstYear = nextLeapYear(firstYear)
	if currentYear < firstYear {
		return 0
	}

	return countLeapYears(firstYear, currentYear)
}

func nextLeapYear(year int) int {
	for !isLeapYear(year) {
		year++
	}

	return year
}

func countLeapYears(startYear, endYear int) int {
	count := 0

	for year := startYear; year <= endYear; year++ {
		if isLeapYear(year) {
			count++
		}
	}

	return count
}

func isLeapYear(year int) bool {
	return year%400 == 0 || (year%4 == 0 && year%100 != 0)
}

func resolveCelebrationMemberName(m *domain.Member) string {
	if m.ShortKoreanName != "" {
		return m.ShortKoreanName
	}

	if m.NameKo != "" {
		return m.NameKo
	}

	return m.Name
}
