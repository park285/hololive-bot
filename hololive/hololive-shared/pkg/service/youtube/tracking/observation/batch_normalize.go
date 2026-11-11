package observation

import "fmt"

func normalizeBatchRecords[T any](
	records []T,
	op string,
	normalize func(T) (T, error),
	identityKey func(T) string,
	merge func(existing, next T) T,
) ([]T, error) {
	normalizedByIdentity := make(map[string]T, len(records))
	normalizedOrder := make([]string, 0, len(records))

	for i, record := range records {
		normalizedRecord, err := normalize(record)
		if err != nil {
			return nil, fmt.Errorf("%s: normalize record at index %d: %w", op, i, err)
		}

		key := identityKey(normalizedRecord)
		if existing, ok := normalizedByIdentity[key]; ok {
			normalizedByIdentity[key] = merge(existing, normalizedRecord)

			continue
		}

		normalizedByIdentity[key] = normalizedRecord
		normalizedOrder = append(normalizedOrder, key)
	}

	normalized := make([]T, 0, len(normalizedOrder))
	for _, key := range normalizedOrder {
		normalized = append(normalized, normalizedByIdentity[key])
	}

	return normalized, nil
}
