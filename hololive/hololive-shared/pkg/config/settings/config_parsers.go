package settings

import (
	"log/slog"
	"strconv"
	"strings"
)

func parseIntList(value string) []int {
	if value == "" {
		return []int{}
	}

	parts := strings.Split(value, ",")
	result := make([]int, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		intVal, err := strconv.Atoi(trimmed)
		if err != nil {
			slog.Warn("config_int_list_entry_dropped",
				slog.String("entry", trimmed),
				slog.Any("error", err))

			continue
		}

		result = append(result, intVal)
	}

	return result
}
