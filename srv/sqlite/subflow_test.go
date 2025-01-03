package sqlite

import (
	"context"

	"sidekick/domain"
	"testing"

	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestPersistSubflow(t *testing.T) {
	storage := NewTestSqliteStorage(t, "subflow_test")
	ctx := context.Background()

	validSubflow := domain.Subflow{
		WorkspaceId: ksuid.New().String(),
		Id:          "sf_" + ksuid.New().String(),
		FlowId:      ksuid.New().String(),
		Name:        "Test Subflow",
		Description: "This is a test subflow",
		Status:      domain.SubflowStatusInProgress,
	}

	t.Run("Successfully persist a valid subflow", func(t *testing.T) {
		err := storage.PersistSubflow(ctx, validSubflow)
		assert.NoError(t, err)

		// Verify the subflow was persisted
		subflows, err := storage.GetSubflows(ctx, validSubflow.WorkspaceId, validSubflow.FlowId)
		assert.NoError(t, err)
		assert.Len(t, subflows, 1)
		assert.Equal(t, validSubflow, subflows[0])
	})

	t.Run("Attempt to persist a subflow with missing required fields", func(t *testing.T) {
		invalidSubflow := validSubflow
		invalidSubflow.WorkspaceId = ""

		err := storage.PersistSubflow(ctx, invalidSubflow)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workspaceId")
	})

	t.Run("Persist and then update an existing subflow", func(t *testing.T) {
		err := storage.PersistSubflow(ctx, validSubflow)
		assert.NoError(t, err)

		updatedSubflow := validSubflow
		updatedSubflow.Status = domain.SubflowStatusComplete
		updatedSubflow.Result = "Completed successfully"

		err = storage.PersistSubflow(ctx, updatedSubflow)
		assert.NoError(t, err)

		// Verify the subflow was updated
		subflows, err := storage.GetSubflows(ctx, validSubflow.WorkspaceId, validSubflow.FlowId)
		assert.NoError(t, err)
		assert.Len(t, subflows, 1)
		assert.Equal(t, updatedSubflow, subflows[0])
	})
}

func TestGetSubflows(t *testing.T) {
	storage := NewTestSqliteStorage(t, "subflow_test")
	ctx := context.Background()

	workspaceId := ksuid.New().String()
	flowId := ksuid.New().String()

	subflows := []domain.Subflow{
		{
			WorkspaceId: workspaceId,
			Id:          "sf_" + ksuid.New().String(),
			FlowId:      flowId,
			Name:        "Subflow 1",
			Status:      domain.SubflowStatusInProgress,
		},
		{
			WorkspaceId: workspaceId,
			Id:          "sf_" + ksuid.New().String(),
			FlowId:      flowId,
			Name:        "Subflow 2",
			Status:      domain.SubflowStatusComplete,
		},
	}

	// Persist test subflows
	for _, sf := range subflows {
		err := storage.PersistSubflow(ctx, sf)
		require.NoError(t, err)
	}

	t.Run("Retrieve multiple subflows for a given workspace and flow", func(t *testing.T) {
		retrievedSubflows, err := storage.GetSubflows(ctx, workspaceId, flowId)
		assert.NoError(t, err)
		assert.Len(t, retrievedSubflows, 2)
		assert.ElementsMatch(t, subflows, retrievedSubflows)
	})

	t.Run("Attempt to retrieve subflows with invalid workspace or flow ID", func(t *testing.T) {
		_, err := storage.GetSubflows(ctx, "", flowId)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workspaceId and flowId cannot be empty")

		_, err = storage.GetSubflows(ctx, workspaceId, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "workspaceId and flowId cannot be empty")
	})

	t.Run("Retrieve subflows when none exist for the given workspace and flow", func(t *testing.T) {
		nonExistentWorkspaceId := ksuid.New().String()
		nonExistentFlowId := ksuid.New().String()

		retrievedSubflows, err := storage.GetSubflows(ctx, nonExistentWorkspaceId, nonExistentFlowId)
		assert.NoError(t, err)
		assert.Len(t, retrievedSubflows, 0)
	})
}
