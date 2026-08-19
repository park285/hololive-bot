package kakaoroom

import (
	"context"
	"fmt"

	"github.com/park285/iris-client-go/v2/iris"
)

type IrisRooms interface {
	GetRooms(ctx context.Context) (*iris.RoomListResponse, error)
}

type irisRooms struct {
	client IrisRooms
}

func NewIrisLister(client IrisRooms) irisLister {
	if client == nil {
		return nil
	}

	return irisRooms{client: client}
}

func (l irisRooms) GetRooms(ctx context.Context) ([]Facts, error) {
	resp, err := l.client.GetRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("iris get rooms: %w", err)
	}
	if resp == nil {
		return nil, nil
	}

	rooms := make([]Facts, 0, len(resp.Rooms))
	for _, summary := range resp.Rooms {
		rooms = append(rooms, factsFromSummary(summary))
	}

	return rooms, nil
}

func ListerFrom(client any) irisLister {
	rooms, ok := client.(IrisRooms)
	if !ok {
		return nil
	}

	return NewIrisLister(rooms)
}
