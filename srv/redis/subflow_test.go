package redis

import (
	"context"
	"fmt"
	"sidekick/domain"
	"testing"

	"github.com/segmentio/ksuid"
	"github.com/stretchr/testify/assert"
)

func TestPersistSubflow(t *testing.T) {
	db := NewTestRedisStorage()
	ctx := context.Background()

	validSubflow := domain.Subflow{
		WorkspaceId: ksuid.New().String(),
		Id:          "sf_" + ksuid.New().String(),
		FlowId:      ksuid.New().String(),
		Name:        "Test Subflow",
		Description: "This is a test subflow",
		Status:      domain.SubflowStatusInProgress,
	}

	tests := []struct {
		name          string
		subflow       domain.Subflow
		expectedError bool
		errorContains string
	}{
		{
			name:          "Successfully persist a valid subflow",
			subflow:       validSubflow,
			expectedError: false,
		},
		{
			name: "Empty WorkspaceId",
			subflow: func() domain.Subflow {
				sf := validSubflow
				sf.WorkspaceId = ""
				return sf
			}(),
			expectedError: true,
			errorContains: "workspaceId",
		},
		{
			name: "Empty Id",
			subflow: func() domain.Subflow {
				sf := validSubflow
				sf.Id = ""
				return sf
			}(),
			expectedError: true,
			errorContains: "subflow.Id",
		},
		{
			name: "Empty FlowId",
			subflow: func() domain.Subflow {
				sf := validSubflow
				sf.FlowId = ""
				return sf
			}(),
			expectedError: true,
			errorContains: "subflow.FlowId",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.PersistSubflow(ctx, tt.subflow)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)

				// Verify the subflow was persisted correctly
				subflowKey := fmt.Sprintf("%s:%s", tt.subflow.WorkspaceId, tt.subflow.Id)
				subflowSetKey := fmt.Sprintf("%s:%s:subflows", tt.subflow.WorkspaceId, tt.subflow.FlowId)

				// Check if the subflow exists in Redis
				exists, err := db.Client.Exists(ctx, subflowKey).Result()
				assert.NoError(t, err)
				assert.Equal(t, int64(1), exists)

				// Check if the subflow ID is in the flow's subflow set
				isMember, err := db.Client.SIsMember(ctx, subflowSetKey, tt.subflow.Id).Result()
				assert.NoError(t, err)
				assert.True(t, isMember)
			}
		})
	}
}

func TestGetSubflows(t *testing.T) {
	db := NewTestRedisStorage()
	ctx := context.Background()

	workspaceId := ksuid.New().String()
	flowId := ksuid.New().String()

	// Create test subflows
	subflows := []domain.Subflow{
		{
			WorkspaceId: workspaceId,
			Id:          "sf_" + ksuid.New().String(),
			Name:        "Subflow 1",
			FlowId:      flowId,
			Status:      domain.SubflowStatusInProgress,
		},
		{
			WorkspaceId: workspaceId,
			Id:          "sf_" + ksuid.New().String(),
			Name:        "Subflow 2",
			FlowId:      flowId,
			Status:      domain.SubflowStatusComplete,
		},
	}

	// Persist test subflows
	for _, sf := range subflows {
		err := db.PersistSubflow(ctx, sf)
		assert.NoError(t, err)
	}

	tests := []struct {
		name           string
		workspaceId    string
		flowId         string
		expectedError  bool
		errorContains  string
		expectedLength int
	}{
		{
			name:           "Successfully retrieving multiple subflows",
			workspaceId:    workspaceId,
			flowId:         flowId,
			expectedError:  false,
			expectedLength: 2,
		},
		{
			name:          "Empty workspaceId",
			workspaceId:   "",
			flowId:        flowId,
			expectedError: true,
			errorContains: "workspaceId and flowId cannot be empty",
		},
		{
			name:          "Empty flowId",
			workspaceId:   workspaceId,
			flowId:        "",
			expectedError: true,
			errorContains: "workspaceId and flowId cannot be empty",
		},
		{
			name:           "Non-existent flow",
			workspaceId:    workspaceId,
			flowId:         ksuid.New().String(),
			expectedError:  false,
			expectedLength: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retrievedSubflows, err := db.GetSubflows(ctx, tt.workspaceId, tt.flowId)

			if tt.expectedError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.Len(t, retrievedSubflows, tt.expectedLength)

				if tt.expectedLength > 0 {
					assert.ElementsMatch(t, subflows, retrievedSubflows)
				}
			}
		})
	}
}