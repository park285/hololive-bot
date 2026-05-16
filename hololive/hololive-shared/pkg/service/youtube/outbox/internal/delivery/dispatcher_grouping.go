package delivery

import "github.com/kapu/hololive-shared/pkg/domain"

type deliveryGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	rows      []domain.YouTubeNotificationDelivery
	outboxes  []domain.YouTubeNotificationOutbox
}

func groupDeliveryRows(
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (groups []deliveryGroup, orphanRows []domain.YouTubeNotificationDelivery) {
	if len(rows) == 0 {
		return nil, nil
	}

	index := make(map[string]int)
	groups = make([]deliveryGroup, 0, len(rows))

	for i := range rows {
		row := rows[i]
		outbox, ok := outboxByID[row.OutboxID]
		if !ok {
			orphanRows = append(orphanRows, row)
			continue
		}

		if outbox.Kind == domain.OutboxKindMilestone {
			groups = append(groups, deliveryGroup{
				roomID:    row.RoomID,
				channelID: outbox.ChannelID,
				kind:      outbox.Kind,
				rows:      []domain.YouTubeNotificationDelivery{row},
				outboxes:  []domain.YouTubeNotificationOutbox{outbox},
			})
			continue
		}

		key := row.RoomID + "|" + outbox.ChannelID + "|" + string(outbox.Kind)
		if idx, exists := index[key]; exists {
			groups[idx].rows = append(groups[idx].rows, row)
			groups[idx].outboxes = append(groups[idx].outboxes, outbox)
			continue
		}

		index[key] = len(groups)
		groups = append(groups, deliveryGroup{
			roomID:    row.RoomID,
			channelID: outbox.ChannelID,
			kind:      outbox.Kind,
			rows:      []domain.YouTubeNotificationDelivery{row},
			outboxes:  []domain.YouTubeNotificationOutbox{outbox},
		})
	}

	return groups, orphanRows
}
