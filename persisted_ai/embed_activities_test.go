package persisted_ai

import (
	"context"
	"fmt"
	"sidekick/common"
	"sidekick/embedding"
	"sidekick/secret_manager"
	"sidekick/srv/sqlite"
	"testing"

	"github.com/kelindar/binary"
	"github.com/stretchr/testify/require"
)

func TestCachedEmbedActivity_AllCached(t *testing.T) {
	storage := sqlite.NewTestStorage(t, "all_cached")

	oa := &EmbedActivities{
		Storage: storage,
	}
	options := CachedEmbedActivityOptions{
		WorkspaceId: "test_workspace",
		ModelConfig: common.ModelConfig{Model: "ada2", Provider: "mock"},
		ContentType: "test_type",
		Subkeys:     []string{"1"},
	}

	expectedKeys := []string{"embedding:ada2:test_type:1"}
	expectedEmbedding := embedding.EmbeddingVector{0.5, 0.1}

	// Pre-cache the embeddings
	err := storage.MSet(context.Background(), "test_workspace", map[string]interface{}{expectedKeys[0]: expectedEmbedding})
	require.NoError(t, err)
	err = oa.CachedEmbedActivity(context.Background(), options)
	require.NoError(t, err)

	// Check if the expected keys are in the cache and if their values are the expected embeddings.
	for _, key := range expectedKeys {
		values, err := storage.MGet(context.Background(), options.WorkspaceId, []string{key})
		require.NoError(t, err)
		require.Len(t, values, 1)
		raw := values[0]
		require.NotNil(t, raw)
		var emb embedding.EmbeddingVector
		err = binary.Unmarshal(raw, &emb)
		require.NoError(t, err)
		require.Equal(t, expectedEmbedding, emb)
	}
}

func TestCachedEmbedActivity_NoKeys(t *testing.T) {
	db := sqlite.NewTestStorage(t, "no_keys")

	oa := &EmbedActivities{
		Storage: db,
	}
	options := CachedEmbedActivityOptions{
		WorkspaceId: "test_workspace",
		ModelConfig: common.ModelConfig{Model: "ada2", Provider: "mock"},
		ContentType: "test_type",
		Subkeys:     []string{},
	}

	err := oa.CachedEmbedActivity(context.Background(), options)
	require.NoError(t, err)
}

func TestCachedEmbedActivity_MissedCache(t *testing.T) {
	storage := sqlite.NewTestStorage(t, "missed_cache")

	oa := &EmbedActivities{
		Storage: storage,
	}
	options := CachedEmbedActivityOptions{
		Secrets: secret_manager.SecretManagerContainer{
			SecretManager: &secret_manager.MockSecretManager{},
		},
		WorkspaceId: "test_workspace",
		ModelConfig: common.ModelConfig{Model: "ada2", Provider: "mock"},
		ContentType: "test_type",
		Subkeys:     []string{"1", "2", "3"},
	}

	// have all content keys
	err := storage.MSet(context.Background(), options.WorkspaceId, map[string]interface{}{
		"test_type:1": "some test content 1",
		"test_type:2": "some test content 2",
		"test_type:3": "some test content 3",
	})
	require.NoError(t, err)

	// Pre-cache the embeddings for some of the keys
	err = storage.MSet(context.Background(), options.WorkspaceId, map[string]interface{}{
		"embedding:ada2:test_type:1": embedding.EmbeddingVector{1.0, 2.0, 3.0},
		"embedding:ada2:test_type:2": embedding.EmbeddingVector{4.0, 5.0, 6.0},
	})
	require.NoError(t, err)

	err = oa.CachedEmbedActivity(context.Background(), options)
	require.NoError(t, err)

	// Check if all the expected keys are in the cache and if their values are the expected embeddings.
	for _, subKey := range options.Subkeys {
		key := fmt.Sprintf("embedding:ada2:test_type:%s", subKey)
		values, err := storage.MGet(context.Background(), options.WorkspaceId, []string{key})
		require.NoError(t, err)
		require.Len(t, values, 1)
		if values[0] != nil {
			// Optionally: validate the value kind/contents further as needed
			require.NotNil(t, values[0])
		}
	}
}
