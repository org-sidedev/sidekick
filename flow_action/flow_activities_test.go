package flow_action

import (
	"context"
	"encoding/json"
	"log"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"sidekick/db"
	"sidekick/models"
)

func newTestRedisDatabase() *db.RedisDatabase {
	db := &db.RedisDatabase{}
	db.Client = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       1,
	})

	// Flush the database synchronously to ensure a clean state for each test
	_, err := db.Client.FlushDB(context.Background()).Result()
	if err != nil {
		log.Panicf("failed to flush redis database: %v", err)
	}

	return db
}

func TestPersistFlowAction(t *testing.T) {
	fa := FlowActivities{
		DatabaseAccessor: newTestRedisDatabase(),
	}

	flowAction := models.FlowAction{
		Id:          "test_id",
		FlowId:      "test_flow_id",
		WorkspaceId: "test_workspace_id",
	}

	err := fa.PersistFlowAction(context.Background(), flowAction)
	assert.NoError(t, err)

	key := "test_workspace_id:test_id"
	val, err := fa.DatabaseAccessor.Client.Get(context.Background(), key).Result()
	assert.NoError(t, err)

	var persistedFlowAction models.FlowAction
	err = json.Unmarshal([]byte(val), &persistedFlowAction)
	assert.NoError(t, err)

	assert.Equal(t, flowAction, persistedFlowAction)
}
