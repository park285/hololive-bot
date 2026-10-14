package observation

import "fmt"

// normalizeBatchRecords는 batch upsert 입력을 정규화하고 같은 identity key의 레코드를 첫 등장 순서대로 하나로 합친다.
// 한 레코드라도 정규화에 실패하면 op와 위치를 담은 오류를 돌려준다.
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
