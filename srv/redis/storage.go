package redis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Storage struct {
	Client *redis.Client
}

func NewStorage() *Storage {
	return &Storage{Client: setupClient()}
}

func (s Storage) CheckConnection(ctx context.Context) error {
	_, err := s.Client.Ping(context.Background()).Result()
	return err
}

func (s Storage) MGet(ctx context.Context, keys []string) ([]interface{}, error) {
	return s.Client.MGet(ctx, keys...).Result()
}

func (s Storage) MSet(ctx context.Context, values map[string]interface{}) error {
	return s.Client.MSet(ctx, values).Err()
}

func toMap(something interface{}) (map[string]interface{}, error) {
	// Convert the thing to JSON
	jsonData, err := json.Marshal(something)
	if err != nil {
		return nil, fmt.Errorf("failed to convert something to JSON: %w", err)
	}

	// Convert the JSON data to a map
	var dataMap map[string]interface{}
	err = json.Unmarshal(jsonData, &dataMap)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON to map: %w", err)
	}

	return dataMap, nil
}
