package dev

import (
	"fmt"
	"path/filepath"
	"sidekick/models"
	"sidekick/utils"
	"strings"
	"time"

	"github.com/segmentio/ksuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type DevAgentManagerWorkflowInput struct {
	WorkspaceId string
}

// DevAgentManagerWorkflow handles signals, requests, and work requests within the provided workflow context.
// It returns any error that occurs in these operations.
func DevAgentManagerWorkflow(ctx workflow.Context, input DevAgentManagerWorkflowInput) error {
	log := workflow.GetLogger(ctx)

	defer func() {
		// this workflow failing is very annoying as no new tasks can be
		// created. we auto-start a new workflow when there isn't a running one,
		// so we'll finish the flow as soon as there is a panic, i.e. any
		// workflow task fails
		if r := recover(); r != nil {
			err, ok := r.(error)
			if !ok {
				log.Error("Panic in DevAgentManagerWorkflow", "Error", err)
			}
		}
	}()

	count := 0
	ctx = setActivityOptions(ctx)
	var ima *DevAgentManagerActivities // use a nil struct pointer to call activities that are part of a structure
	future, settable := workflow.NewFuture(ctx)
	requests := make(map[string]RequestForUser)
	requestNotifications := make(map[string]bool)

	workflow.Go(ctx, func(ctx workflow.Context) {
		err := handleSignals(ctx, input, ima, &count, &requests, &requestNotifications)
		settable.Set(nil, err)
	})

	err := handleWorkRequests(ctx, input.WorkspaceId, ima, &count)
	if err != nil {
		return err
	}

	err = future.Get(ctx, nil)
	return err
}

type CancelSignal struct {
	TopicId    string
	WorkflowId string
}

type WorkRequest struct {
	ParentId    string
	Input       string
	FlowType    string
	FlowOptions map[string]interface{}
}

type WorkRequestResult struct {
	WorkflowId string
}

const SignalNameCancel = "cancel"

func handleCancel(ctx workflow.Context, c workflow.ReceiveChannel, ima *DevAgentManagerActivities) {
	// FIXME remove the argument and use the below commented out code instead
	// var ima *DevAgentManagerActivities // use a nil struct pointer to call activities that are part of a structure
	ctx = setActivityOptions(ctx)
	log := workflow.GetLogger(ctx)
	log.Info("Cancel signal received")
	var cancelSignal CancelSignal
	c.Receive(ctx, &cancelSignal)

	err := workflow.RequestCancelExternalWorkflow(ctx, cancelSignal.WorkflowId, "").Get(ctx, nil)
	if err != nil {
		// TODO send a signal to the user that the cancellation failed and to retry
		log.Error("Failed to send cancellation request", "Error", err)
	}

	// FIXME below is broken as it needs to use ExecuteActivity instead
	// Also, it should add a new UpdateWorkflowStatus activity
	// Also, it should create a message record so the user sees feedback showing this happened
	// // Fetch the existing workflow record
	// record, err := ima.GetWorkflow(ctx, cancelSignal.WorkflowId)
	// if err != nil {
	// 	log.Error("Failed to fetch workflow record", "Error", err)
	// 	return
	// }

	// // Update the workflow record status to "Cancelled"
	// record.Status = "Cancelled"

	// // Update the workflow record in the db
	// err = ima.PutWorkflow(ctx, record)
	// if err != nil {
	// 	log.Error("Failed to update workflow record", "Error", err)
	// }
}

func handleRequestForUser(ctx workflow.Context, c workflow.ReceiveChannel, input DevAgentManagerWorkflowInput, requests *map[string]RequestForUser) {
	var ima *DevAgentManagerActivities // use a nil struct pointer to call activities that are part of a structure

	workspaceId := input.WorkspaceId
	ctx = setActivityOptions(ctx)
	log := workflow.GetLogger(ctx)
	log.Info("Request for user signal received")
	var req RequestForUser
	c.Receive(ctx, &req)

	var flow models.Flow
	err := workflow.ExecuteActivity(ctx, ima.GetWorkflow, workspaceId, req.OriginWorkflowId).Get(ctx, &flow)
	if err != nil {
		log.Error("Failed to retrieve workflow record", "Error", err)
		return
	}

	if strings.HasPrefix(flow.ParentId, "task_") {
		err := workflow.ExecuteActivity(ctx, ima.CreatePendingUserRequest, workspaceId, req).Get(ctx, nil)
		if err != nil {
			log.Error("Failed to execute CreatePendingUserRequest activity", "Error", err)
			return
		}

		err = workflow.ExecuteActivity(ctx, ima.UpdateTaskForUserRequest, workspaceId, req.OriginWorkflowId).Get(ctx, nil)
		if err != nil {
			log.Error("Failed to execute UpdateTaskForUserRequest activity", "Error", err)
			return
		}
	} else {
		// we just record the request here. a separate concurrent loop in the
		// workflow actually passes this request on to the user
		// NOTE there can only be one request per workflow right now, as a given
		// workflow stops while it waits for this request to be fulfilled
		(*requests)[req.OriginWorkflowId] = RequestForUser{
			OriginWorkflowId: req.OriginWorkflowId,
			Content:          req.Content,
			RequestParams:    req.RequestParams, // Use the options field from the request
			RequestKind:      req.RequestKind,   // Use the requestKind field from the request
		}
	}
}

// XXX this can just go directly from the agent perform action to the relevant
// workflow. is there a need to track requests here? i guess just to keep track
// of them? Actually, that does seem helpful, so we can follow up when requests
// are taking a long time to fulfill for example.

func handleUserResponse(ctx workflow.Context, c workflow.ReceiveChannel, ima *DevAgentManagerActivities, requests *map[string]RequestForUser, requestNotifications *map[string]bool) {
	// FIXME remove the argument and use the below commented out code instead
	// var ima *DevAgentManagerActivities // use a nil struct pointer to call activities that are part of a structure
	ctx = setActivityOptions(ctx)
	log := workflow.GetLogger(ctx)
	log.Info("User response signal received")
	var userResponse UserResponse
	c.Receive(ctx, &userResponse)

	// NOTE: it's expected that the workflow we're signaling will handle
	// different types of user responses based on the userRequest's RequestKind
	// and Options etc. The workflow manager's job is just to pass on the user
	// response without any further processing or business logic.
	err := workflow.ExecuteActivity(ctx, ima.PassOnUserResponse, userResponse).Get(ctx, nil)
	if err != nil {
		log.Error("Failed to pass on user response", "Error", err)
	}

	// NOTE there can only be one request per workflow right now, as a given
	// workflow stops while it waits for this request to be fulfilled
	delete(*requests, userResponse.TargetWorkflowId)
	delete(*requestNotifications, userResponse.TargetWorkflowId)
}

func handleWorkflowClosure(ctx workflow.Context, c workflow.ReceiveChannel, input DevAgentManagerWorkflowInput, ima *DevAgentManagerActivities) {
	var closure WorkflowClosure
	c.Receive(ctx, &closure)
	log := workflow.GetLogger(ctx)
	log.Info("Received workflow closure", "closure", closure)

	// Update the Flow status to completed
	// FIXME execute activity instead
	var flow models.Flow
	err := workflow.ExecuteActivity(ctx, ima.GetWorkflow, input.WorkspaceId, closure.FlowId).Get(ctx, &flow)
	if err != nil {
		log.Error("Failed to get workflow", "Error", err)
		return
	}
	flow.Status = closure.Reason // TODO rename Reason to Status
	err = workflow.ExecuteActivity(ctx, ima.PutWorkflow, flow).Get(ctx, nil)
	if err != nil {
		log.Error("Failed to update workflow status", "Error", err)
		return
	}

	// If the parentId starts with "task_", we should retrieve the task and update its status to complete and agent type to "none"
	if strings.HasPrefix(flow.ParentId, "task_") {
		err = workflow.ExecuteActivity(ctx, ima.CompleteFlowParentTask, input.WorkspaceId, flow.ParentId, flow.Status).Get(ctx, nil)
		if err != nil {
			log.Error("Failed to complete parent task", "Error", err)
			return
		}
	}
}

func handleSignals(ctx workflow.Context, input DevAgentManagerWorkflowInput, ima *DevAgentManagerActivities, count *int, requests *map[string]RequestForUser, requestNotifications *map[string]bool) error {
	// FIXME remove the argument and use the below commented out code instead
	// var ima *DevAgentManagerActivities // use a nil struct pointer to call activities that are part of a structure
	cancelSigChan := workflow.GetSignalChannel(ctx, SignalNameCancel)
	requestForUserSigChan := workflow.GetSignalChannel(ctx, SignalNameRequestForUser)
	userResponseSigChan := workflow.GetSignalChannel(ctx, SignalNameUserResponse)
	workflowClosedSignalChan := workflow.GetSignalChannel(ctx, SignalNameWorkflowClosed)

	for {
		selector := workflow.NewNamedSelector(ctx, "signalSelector")
		selector.AddReceive(cancelSigChan, func(c workflow.ReceiveChannel, _ bool) { handleCancel(ctx, c, ima) })
		selector.AddReceive(requestForUserSigChan, func(c workflow.ReceiveChannel, _ bool) { handleRequestForUser(ctx, c, input, requests) })
		selector.AddReceive(userResponseSigChan, func(c workflow.ReceiveChannel, _ bool) {
			handleUserResponse(ctx, c, ima, requests, requestNotifications)
		})
		selector.AddReceive(workflowClosedSignalChan, func(c workflow.ReceiveChannel, _ bool) { handleWorkflowClosure(ctx, c, input, ima) })
		selector.Select(ctx)

		*count++
		if *count >= 1000 && !selector.HasPending() {
			// XXX despite checking HasPending, do we still need to do an async drain of signals??
			// FIXME pass on the requests map as part of the input
			err := workflow.NewContinueAsNewError(ctx, DevAgentManagerWorkflow, input)
			return err
		}
	}
}

// workflow-safe ksuid generation via a side effect
func ksuidSideEffect(ctx workflow.Context) string {
	encodedKsuid := workflow.SideEffect(ctx, func(ctx workflow.Context) interface{} {
		return ksuid.New().String()
	})
	var ksuidValue string
	encodedKsuid.Get(&ksuidValue)
	return ksuidValue
}

func executeWorkRequest(ctx workflow.Context, workspaceId string, workRequest WorkRequest, ima *DevAgentManagerActivities) (models.Flow, error) {
	// FIXME remove the argument and use the below commented out code instead
	// var ima *DevAgentManagerActivities // use a nil struct pointer to call activities that are part of a structure

	var workspace models.Workspace
	err := workflow.ExecuteActivity(ctx, ima.FindWorkspaceById, workspaceId).Get(ctx, &workspace)
	if err != nil {
		return models.Flow{}, err
	}

	repoDir, err := filepath.Abs(workspace.LocalRepoDir) // TODO specify sandbox to run these things in instead later
	if err != nil {
		return models.Flow{}, err
	}

	log := workflow.GetLogger(ctx)

	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:        "flow_" + ksuidSideEffect(ctx),
		ParentClosePolicy: enums.PARENT_CLOSE_POLICY_ABANDON,
	})

	// TODO consider creating the requested workflow in an activity and making
	// it an unrelated workflow rather than a child workflow: we aren't using
	// the fact that it is a child workflow really, unless we're using the
	// parent workflow id somewhere
	var childWorkflowFuture workflow.ChildWorkflowFuture
	untypedOptions := workRequest.FlowOptions
	if workRequest.FlowType == "basic_dev" {
		var options BasicDevOptions
		utils.Transcode(untypedOptions, &options)
		childWorkflowFuture = workflow.ExecuteChildWorkflow(childCtx, BasicDevWorkflow, BasicDevWorkflowInput{
			WorkspaceId:     workspaceId,
			Requirements:    workRequest.Input,
			RepoDir:         repoDir,
			BasicDevOptions: options,
		})
	} else if workRequest.FlowType == "planned_dev" {
		var options PlannedDevOptions
		utils.Transcode(untypedOptions, &options)
		childWorkflowFuture = workflow.ExecuteChildWorkflow(childCtx, PlannedDevWorkflow, PlannedDevInput{
			WorkspaceId:       workspaceId,
			Requirements:      workRequest.Input,
			RepoDir:           repoDir,
			PlannedDevOptions: options,
		})
	} else {
		log.Error("Invalid flow type", "FlowType", workRequest.FlowType)
		return models.Flow{}, fmt.Errorf("Invalid flow type '%s'. Valid values are 'basic_dev' and 'planned_dev'", workRequest.FlowType)
	}

	var we workflow.Execution
	err = childWorkflowFuture.GetChildWorkflowExecution().Get(childCtx, &we)
	if err != nil {
		log.Error("Child workflow failed to start", "Error", err, "WorkflowType", workRequest.FlowType)
		return models.Flow{}, err
	}
	log.Info("Child workflow started", "WorkflowId", we.ID)
	flow := models.Flow{
		WorkspaceId: workspaceId,
		Id:          we.ID,
		Type:        workRequest.FlowType,
		TopicId:     workRequest.ParentId, // TODO remove
		Status:      "in_progress",
		ParentId:    workRequest.ParentId,
	}
	log.Info("workflow record: %s\n", utils.PrettyJSON(flow))
	err = workflow.ExecuteActivity(ctx, ima.PutWorkflow, flow).Get(ctx, nil)
	if err != nil {
		log.Error("Child workflow record failed to be persisted", "Error", err, "WorkflowId", we.ID)
		return models.Flow{}, err
	}
	return flow, nil
}

const UpdateNameWorkRequest = "workRequest"

func handleWorkRequests(ctx workflow.Context, workspaceId string, ima *DevAgentManagerActivities, count *int) error {
	// FIXME remove the argument and use the below commented out code instead
	// var ima *DevAgentManagerActivities // use a nil struct pointer to call activities that are part of a structure
	err := workflow.SetUpdateHandlerWithOptions(
		ctx, UpdateNameWorkRequest,
		func(ctx workflow.Context, workRequest WorkRequest) (models.Flow, error) {
			*count++
			ctx = setActivityOptions(ctx)
			return executeWorkRequest(ctx, workspaceId, workRequest, ima)
		},
		workflow.UpdateHandlerOptions{},
	)
	return err
}

func setActivityOptions(ctx workflow.Context) workflow.Context {
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		//ScheduleToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Second,
			BackoffCoefficient:     2.0,
			MaximumInterval:        10 * time.Second,
			MaximumAttempts:        3,          // up to 3 retries
			NonRetryableErrorTypes: []string{}, // TODO make out-of-bounds errors non-retryable
		},
	}
	return workflow.WithActivityOptions(ctx, activityOptions)
}
