package redis

import (
	"context"
	"sidekick/domain"
	"sidekick/srv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorktreeStorage(t *testing.T) {
	ctx := context.Background()
	storage := NewTestRedisStorage()

	t.Run("PersistWorktree", func(t *testing.T) {
		worktree := domain.Worktree{
			Id:          "wt_test1",
			FlowId:      "flow1",
			Name:        "Test Worktree",
			Created:     time.Now().UTC(),
			WorkspaceId: "workspace1",
		}

		err := storage.PersistWorktree(ctx, worktree)
		require.NoError(t, err)

		// Verify the worktree was persisted
		persistedWorktree, err := storage.GetWorktree(ctx, worktree.WorkspaceId, worktree.Id)
		require.NoError(t, err)
		assert.Equal(t, worktree, persistedWorktree)
	})

	t.Run("GetWorktree", func(t *testing.T) {
		worktree := domain.Worktree{
			Id:          "wt_test2",
			FlowId:      "flow2",
			Name:        "Test Worktree 2",
			Created:     time.Now().UTC(),
			WorkspaceId: "workspace1",
		}

		err := storage.PersistWorktree(ctx, worktree)
		require.NoError(t, err)

		retrievedWorktree, err := storage.GetWorktree(ctx, worktree.WorkspaceId, worktree.Id)
		require.NoError(t, err)
		assert.Equal(t, worktree, retrievedWorktree)

		// Test non-existent worktree
		_, err = storage.GetWorktree(ctx, "non-existent", "wt_non-existent")
		assert.ErrorIs(t, err, srv.ErrNotFound)
	})

	t.Run("GetWorktrees", func(t *testing.T) {
		workspaceId := "workspace2"
		worktrees := []domain.Worktree{
			{Id: "wt_test3", FlowId: "flow3", Name: "Test Worktree 3", Created: time.Now().UTC(), WorkspaceId: workspaceId},
			{Id: "wt_test4", FlowId: "flow4", Name: "Test Worktree 4", Created: time.Now().UTC(), WorkspaceId: workspaceId},
		}

		for _, wt := range worktrees {
			err := storage.PersistWorktree(ctx, wt)
			require.NoError(t, err)
		}

		retrievedWorktrees, err := storage.GetWorktrees(ctx, workspaceId)
		require.NoError(t, err)
		assert.Len(t, retrievedWorktrees, len(worktrees))

		for _, wt := range worktrees {
			assert.Contains(t, retrievedWorktrees, wt)
		}

		// Test empty workspace
		emptyWorktrees, err := storage.GetWorktrees(ctx, "empty-workspace")
		require.NoError(t, err)
		assert.Empty(t, emptyWorktrees)
	})

	t.Run("DeleteWorktree", func(t *testing.T) {
		worktree := domain.Worktree{
			Id:          "wt_test5",
			FlowId:      "flow5",
			Name:        "Test Worktree 5",
			Created:     time.Now().UTC(),
			WorkspaceId: "workspace3",
		}

		err := storage.PersistWorktree(ctx, worktree)
		require.NoError(t, err)

		err = storage.DeleteWorktree(ctx, worktree.WorkspaceId, worktree.Id)
		require.NoError(t, err)

		// Verify the worktree was deleted
		_, err = storage.GetWorktree(ctx, worktree.WorkspaceId, worktree.Id)
		assert.ErrorIs(t, err, srv.ErrNotFound)

		// Verify the worktree was removed from the workspace set
		worktrees, err := storage.GetWorktrees(ctx, worktree.WorkspaceId)
		require.NoError(t, err)
		assert.NotContains(t, worktrees, worktree)
	})
}