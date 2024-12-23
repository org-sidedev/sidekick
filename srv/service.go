package srv

import (
	"context"
	"sidekick/domain"
)

type Service interface {
	Storage
	Streamer
}

type Storage interface {
	domain.TaskStorage
	domain.FlowStorage
	domain.SubflowStorage
	domain.FlowActionStorage
	domain.WorkspaceStorage

	CheckConnection(ctx context.Context) error

	// TODO add workspaceId to this
	MGet(ctx context.Context, keys []string) ([]interface{}, error)
	// TODO add workspaceId to this
	MSet(ctx context.Context, values map[string]interface{}) error
}

type Streamer interface {
	domain.TaskStreamer
	domain.FlowActionStreamer
	domain.FlowEventStreamer
}
