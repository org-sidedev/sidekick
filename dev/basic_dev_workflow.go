package dev

import (
	"errors"
	"fmt"
	"strings"

	"go.temporal.io/sdk/workflow"

	"sidekick/coding/git"
	"sidekick/common"
	"sidekick/domain"
	"sidekick/env"
	"sidekick/flow_action"
	"sidekick/llm"
	"sidekick/utils"
)

// Define the workflow input type.
type BasicDevWorkflowInput struct {
	WorkspaceId  string
	RepoDir      string
	Requirements string
	BasicDevOptions
}

type BasicDevOptions struct {
	DetermineRequirements bool        `json:"determineRequirements"`
	EnvType               env.EnvType `json:"envType,omitempty" default:"local"`
	StartBranch           *string     `json:"startBranch,omitempty"`
}

func BasicDevWorkflow(ctx workflow.Context, input BasicDevWorkflowInput) (result string, err error) {
	globalState := &GlobalState{}

	// don't recover panics in development so we can debug via temporal UI, at
	// the cost of failed tasks appearing stuck without UI feedback in sidekick
	if SideAppEnv != "development" {
		defer func() {
			// panics should not be used for control flow, but if we do panic, we
			// want to make the error visible in the Sidekick UI and mark the task
			// as failed
			if r := recover(); r != nil {
				_ = signalWorkflowClosure(ctx, "failed")
				var ok bool
				err, ok = r.(error)
				if !ok {
					err = fmt.Errorf("panic: %v", r)
				}
				// TODO create a flow event that will be displayed in the UI
			}
		}()
	}

	ctx = utils.DefaultRetryCtx(ctx)

	dCtx, err := SetupDevContext(ctx, input.WorkspaceId, input.RepoDir, string(input.EnvType), input.BasicDevOptions.StartBranch)
	if err != nil {
		_ = signalWorkflowClosure(ctx, "failed")
		return "", err
	}
	dCtx.GlobalState = globalState

	// Set up the pause and user action handlers
	SetupPauseHandler(dCtx, "Paused for user input", nil)
	SetupUserActionHandler(dCtx)

	// TODO move environment creation to an activity within EnsurePrerequisites
	err = EnsurePrerequisites(dCtx)
	if err != nil {
		_ = signalWorkflowClosure(ctx, "failed")
		return "", err
	}

	requirements := input.Requirements
	if input.DetermineRequirements {
		devRequirements, err := BuildDevRequirements(dCtx, InitialDevRequirementsInfo{Requirements: requirements})
		if err != nil {
			_ = signalWorkflowClosure(dCtx, "failed")
			return "", err
		}
		requirements = devRequirements.String()
	}

	v := workflow.GetVersion(dCtx, "basic-dev-parent-subflow", workflow.DefaultVersion, 1)
	if v == 1 {
		return RunSubflow(dCtx, "coding", "Coding", func(subflow domain.Subflow) (string, error) {
			return codingSubflow(dCtx, requirements, input.EnvType, input.BasicDevOptions.StartBranch)
		})
	} else {
		return codingSubflow(dCtx, requirements, input.EnvType, input.BasicDevOptions.StartBranch)
	}
}

func codingSubflow(dCtx DevContext, requirements string, envType env.EnvType, startBranch *string) (result string, err error) {
	codeContext, fullCodeContext, err := PrepareInitialCodeContext(dCtx, requirements, nil, nil)
	contextSizeExtension := len(fullCodeContext) - len(codeContext)
	if err != nil {
		_ = signalWorkflowClosure(dCtx, "failed")
		return "", fmt.Errorf("failed to prepare code context: %v", err)
	}
	testResult := TestResult{Output: ""}

	// TODO wrap chatHistory in custom struct that has additional metadata about
	// each message for easy manipulation of chat history and determining when
	// we need to re-inject requirements and code context that gets lost
	// TODO store chat history in a way that can be referred to by id, and pass
	// id to the activities to avoid bloating temporal db
	chatHistory := &[]llm.ChatMessage{}

	maxAttempts := 17
	repoConfig := dCtx.RepoConfig
	if repoConfig.MaxIterations > 0 {
		maxAttempts = repoConfig.MaxIterations
	}

	attemptCount := 0
	var promptInfo PromptInfo
	initialCodeInfo := InitialCodeInfo{CodeContext: codeContext, Requirements: requirements}
	promptInfo = initialCodeInfo
	var fulfillment CriteriaFulfillment
	for {
		overallName := "Basic Dev"
		subflowName := fmt.Sprintf("%s (%d)", overallName, attemptCount+1)
		if subflowName == fmt.Sprintf("%s (1)", overallName) {
			subflowName = overallName
		}
		dCtx.FlowScope = &flow_action.FlowScope{SubflowName: subflowName}

		// TODO /gen use models slice and modelIndex and modelAttemptCount just like
		// in completeDevStep to switch models when ErrMaxIterationsReached
		modelConfig := dCtx.GetModelConfig(common.CodingKey, attemptCount/3, "default")

		// TODO don't force getting help if it just got help recently already
		if attemptCount > 0 && attemptCount%3 == 0 {
			guidanceContext := "Failing repeatedly to pass tests and/or fulfill requirements, please provide guidance."

			// get the latest git diff, since it could be different from the
			// last time we got it, if we ever did
			gitDiff, diffErr := git.GitDiff(dCtx.ExecContext)
			if diffErr != nil {
				return "", fmt.Errorf("failed to get git diff: %v", diffErr)
			}

			requestParams := map[string]any{
				"gitDiff":     gitDiff,
				"testResult":  testResult,  // always latest
				"fulfillment": fulfillment, // always latest
			}

			promptInfo, err = GetUserFeedback(dCtx, promptInfo, guidanceContext, chatHistory, requestParams)
			if err != nil {
				_ = signalWorkflowClosure(dCtx, "failed")
				return "", fmt.Errorf("failed to get user feedback: %v", err)
			}
		}
		if attemptCount >= maxAttempts {
			_ = signalWorkflowClosure(dCtx, "failed")
			return "", errors.New("failed to author code passing tests and fulfilling requirements, max attempts reached")
		}

		// Step 2: edit code
		err = EditCode(dCtx, modelConfig, contextSizeExtension, chatHistory, promptInfo)
		if err != nil {
			_ = signalWorkflowClosure(dCtx, "failed")
			return "", fmt.Errorf("failed to write edit blocks: %v", err)
		}

		// Step 3: run tests
		testResult, err = RunTests(dCtx, dCtx.RepoConfig.TestCommands)
		if err != nil {
			_ = signalWorkflowClosure(dCtx, "failed")
			return "", fmt.Errorf("failed to run tests: %v", err)
		}

		if !testResult.TestsPassed {
			promptInfo = FeedbackInfo{Feedback: testResult.Output}
			attemptCount++
			continue
		}

		// Run integration tests if regular tests passed and integration tests are configured
		if len(dCtx.RepoConfig.IntegrationTestCommands) > 0 {
			integrationTestResult, err := RunTests(dCtx, dCtx.RepoConfig.IntegrationTestCommands)
			if err != nil {
				_ = signalWorkflowClosure(dCtx, "failed")
				return "", fmt.Errorf("failed to run integration tests: %v", err)
			}
			if !integrationTestResult.TestsPassed {
				promptInfo = FeedbackInfo{Feedback: integrationTestResult.Output}
				attemptCount++
				continue
			}
		}

		// Step 4: check diff and confirm if requirements have been met
		fulfillment, err = CheckWorkMeetsCriteria(dCtx, CheckWorkInfo{
			Requirements: requirements,
		})
		if err != nil {
			_ = signalWorkflowClosure(dCtx, "failed")
			return "", fmt.Errorf("failed to check if requirements are fulfilled: %v", err)
		}
		if fulfillment.IsFulfilled {
			break
		} else {
			// when we get back that requirements are not fulfilled, we often
			// get an immediate call to get_help_or_input trying to understand
			// why requirements aren't fulfilled or what to do. this is usually
			// not necessary.

			// we already have the requirements fulfillment analysis, so the
			// additional feedback is not needed if we include that in the chat
			// history. we skip saying any more from the user or system role in
			// this case and let the assistant "prompt itself". hoping this
			// makes it less likely for the assistant to act confused, since it
			// knows it itself said the requirements weren't fulfilled and why.
			*chatHistory = append(*chatHistory, llm.ChatMessage{
				Role: llm.ChatMessageRoleUser,
				Content: fmt.Sprintf(`
Here is the diff:

  [...] (Omitted for length)

And here are test results:

  Tests Passed: %v
  [...] (Omitted for length)

Please analyze whether the requirements have been fulfilled. If not, continue editing code as needed.
`, testResult.TestsPassed),
			})
			*chatHistory = append(*chatHistory, llm.ChatMessage{
				Role: llm.ChatMessageRoleAssistant,
				Content: fmt.Sprintf(`
The requirements were not fulfilled.

Analysis: %s

Feedback: %s`, fulfillment.Analysis, fulfillment.FeedbackMessage),
			})
			promptInfo = SkipInfo{}
			attemptCount++
			continue
		}
	}

	// Step 5: auto-format code
	err = AutoFormatCode(dCtx)
	if err != nil {
		_ = signalWorkflowClosure(dCtx, "failed")
		return "", err
	}

	// Step 6: Handle merge if using worktree and workflow version is new enough
	workflowVersion := workflow.GetVersion(dCtx, "git-worktree-merge", workflow.DefaultVersion, 1)
	gitAddVersion := workflow.GetVersion(dCtx, "git-add-before-diff", workflow.DefaultVersion, 1)
	if envType == env.EnvTypeLocalGitWorktree && workflowVersion == 1 {
		defaultTarget := "main"
		if startBranch != nil {
			defaultTarget = *startBranch
		}

		// Ensure any auto-formatted changes are staged for new workflow versions
		if gitAddVersion == 1 {
			if err := git.GitAddAll(dCtx.ExecContext); err != nil {
				_ = signalWorkflowClosure(dCtx, "failed")
				return "", fmt.Errorf("failed to git add all: %v", err)
			}
		}

		gitDiff, diffErr := git.GitDiff(dCtx.ExecContext)
		if diffErr != nil {
			_ = signalWorkflowClosure(dCtx, "failed")
			return "", fmt.Errorf("failed to get git diff: %v", diffErr)
		}

		mergeInfo, err := getMergeApproval(dCtx, defaultTarget, gitDiff)
		if err != nil {
			_ = signalWorkflowClosure(dCtx, "failed")
			return "", fmt.Errorf("failed to get merge approval: %v", err)
		}

		if mergeInfo.Approved {
			// Commit any pending changes first
			message := strings.TrimSpace(requirements)
			if strings.Contains(message, "Overview:\n") {
				message = strings.Split(message, "Overview:\n")[1]
				message = strings.TrimSpace(message)
			}
			message = strings.Split(message, "\n")[0]
			if len(message) > 100 {
				message = message[:100] + "...\n\n..." + message[100:]
			}
			err = workflow.ExecuteActivity(dCtx, git.GitCommitActivity, dCtx.EnvContainer, git.GitCommitParams{
				CommitMessage: message,
			}).Get(dCtx, nil)
			if err != nil {
				_ = signalWorkflowClosure(dCtx, "failed")
				return "", fmt.Errorf("failed to commit changes: %v", err)
			}

			// Perform merge
			actionCtx := dCtx.NewActionContext("merge")
			actionCtx.ActionParams = map[string]interface{}{
				"sourceBranch": dCtx.Worktree.Name,
				"targetBranch": mergeInfo.TargetBranch,
			}
			mergeResult, err := Track(actionCtx, func(flowAction domain.FlowAction) (git.MergeActivityResult, error) {
				var result git.MergeActivityResult
				future := workflow.ExecuteActivity(dCtx, git.GitMergeActivity, dCtx.EnvContainer, git.GitMergeParams{
					SourceBranch: dCtx.Worktree.Name,
					TargetBranch: mergeInfo.TargetBranch,
				})
				err := future.Get(dCtx, &result)
				if err != nil {
					return result, fmt.Errorf("failed to merge branches: %v", err)
				}
				return result, nil
			})
			if err != nil {
				_ = signalWorkflowClosure(dCtx, "failed")
				return "", err
			}

			if mergeResult.HasConflicts {
				// Present continue request with Done tag
				actionCtx := dCtx.NewActionContext("user_request.continue")
				err := GetUserContinue(actionCtx, "Merge conflicts detected. Please resolve conflicts and continue when done.", map[string]any{
					"continueTag": "done",
				})
				if err != nil {
					_ = signalWorkflowClosure(dCtx, "failed")
					return "", fmt.Errorf("failed to get continue approval: %v", err)
				}
			}

			// After successful merge, cleanup the worktree
			if !mergeResult.HasConflicts && dCtx.Worktree != nil {
				actionCtx := dCtx.NewActionContext("cleanup_worktree")
				_, err := Track(actionCtx, func(flowAction domain.FlowAction) (interface{}, error) {
					future := workflow.ExecuteActivity(dCtx, git.CleanupWorktreeActivity, dCtx.EnvContainer, dCtx.EnvContainer.Env.GetWorkingDirectory(), dCtx.Worktree.Name)
					return nil, future.Get(dCtx, nil)
				})
				if err != nil {
					// Log the error but don't fail the workflow since merge was successful
					workflow.GetLogger(dCtx).Error("Failed to cleanup worktree", "error", err)
				}
			}
		}
	}

	// Emit signal when workflow ends successfully
	err = signalWorkflowClosure(dCtx, "completed")
	if err != nil {
		return "", fmt.Errorf("failed to signal workflow closure: %v", err)
	}

	return testResult.Output, nil
}

func getMergeApproval(dCtx DevContext, defaultTarget string, gitDiff string) (MergeApprovalResponse, error) {
	// Request merge approval from user
	mergeParams := MergeApprovalParams{
		SourceBranch:        dCtx.Worktree.Name,
		DefaultTargetBranch: defaultTarget,
		Diff:                gitDiff,
	}

	return GetUserMergeApproval(dCtx, "Please approve before we merge", map[string]any{
		"mergeApprovalInfo": mergeParams,
	})
}
