package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sidekick/common"
	"sidekick/domain"
)

// Ensure Storage implements FlowStorage interface
var _ domain.FlowStorage = (*Storage)(nil)

func (s *Storage) PersistFlow(ctx context.Context, flow domain.Flow) error {
	query := `
		INSERT OR REPLACE INTO flows (workspace_id, id, type, parent_id, status)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		flow.WorkspaceId, flow.Id, flow.Type, flow.ParentId, flow.Status,
	)
	if err != nil {
		return fmt.Errorf("failed to persist flow: %w", err)
	}

	return nil
}

func (s *Storage) GetFlow(ctx context.Context, workspaceId, flowId string) (domain.Flow, error) {
	query := `
		SELECT workspace_id, id, type, parent_id, status
		FROM flows
		WHERE workspace_id = ? AND id = ?
	`

	var flow domain.Flow
	err := s.db.QueryRowContext(ctx, query, workspaceId, flowId).Scan(
		&flow.WorkspaceId, &flow.Id, &flow.Type, &flow.ParentId, &flow.Status)

	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Flow{}, common.ErrNotFound
		}
		return domain.Flow{}, fmt.Errorf("failed to get flow: %w", err)
	}

	return flow, nil
}

func (s *Storage) GetFlowsForTask(ctx context.Context, workspaceId, taskId string) ([]domain.Flow, error) {
	query := `
		SELECT workspace_id, id, type, parent_id, status
		FROM flows
		WHERE workspace_id = ? AND parent_id = ?
	`

	rows, err := s.db.QueryContext(ctx, query, workspaceId, taskId)
	if err != nil {
		return nil, fmt.Errorf("failed to query flows for task: %w", err)
	}
	defer rows.Close()

	flows := make([]domain.Flow, 0)
	for rows.Next() {
		var flow domain.Flow
		err := rows.Scan(&flow.WorkspaceId, &flow.Id, &flow.Type, &flow.ParentId, &flow.Status)
		if err != nil {
			return nil, fmt.Errorf("failed to scan flow row: %w", err)
		}
		flows = append(flows, flow)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating flow rows: %w", err)
	}

	return flows, nil
}
