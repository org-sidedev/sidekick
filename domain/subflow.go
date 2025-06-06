package domain

import "context"

type SubflowStatus string

const (
	SubflowStatusStarted  SubflowStatus = "started" // this more-or-less means "in-progress"
	SubflowStatusComplete SubflowStatus = "complete"
	SubflowStatusFailed   SubflowStatus = "failed"
)

// Subflow represents a subflow within a flow
type Subflow struct {
	WorkspaceId     string        `json:"workspaceId"`
	Id              string        `json:"id"`                        // Unique identifier, prefixed with 'sf_'
	Name            string        `json:"name"`                      // Name of the subflow
	Type            *string       `json:"type,omitempty"`            // Type of the subflow (e.g., "step" or "edit_code")
	Description     string        `json:"description,omitempty"`     // Description of the subflow, if any
	Status          SubflowStatus `json:"status"`                    // Status of the subflow
	ParentSubflowId string        `json:"parentSubflowId,omitempty"` // ID of the parent subflow, if any
	FlowId          string        `json:"flowId"`                    // ID of the flow this subflow belongs to
	Result          string        `json:"result,omitempty"`          // Result of the subflow, if any
}

type SubflowStorage interface {
	PersistSubflow(ctx context.Context, subflow Subflow) error
	GetSubflows(ctx context.Context, workspaceId, flowId string) ([]Subflow, error)
	GetSubflow(ctx context.Context, workspaceId, subflowId string) (Subflow, error)
}
