package sidekick

import (
	"encoding/json"
	"fmt"
	"sidekick/common"
	"sidekick/llm"
	"sidekick/persisted_ai"
	"sidekick/secret_manager"
	"sidekick/srv/redis"
	"time"

	"github.com/ehsanul/anthropic-go/v3/pkg/anthropic"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type SentimentRequest struct {
	Analysis  string `json:"analysis" jsonschema:"description=Very brief analysis of the sentiment of the user's message"`
	Sentiment string `json:"sentiment" jsonschema:"enum=positive,enum=negative"`
}

func ExampleLlmActivitiesWorkflow(ctx workflow.Context) (string, error) {
	// RetryPolicy specifies how to automatically handle retries if an Activity fails.
	retrypolicy := &temporal.RetryPolicy{
		InitialInterval:        time.Second,
		BackoffCoefficient:     2.0,
		MaximumInterval:        100 * time.Second,
		MaximumAttempts:        1, // don't retry
		NonRetryableErrorTypes: []string{"SomeApplicationError", "AnotherApplicationError"},
	}

	activityOptions := workflow.ActivityOptions{
		// Timeout options specify when to automatically timeout Activity functions.
		StartToCloseTimeout: time.Minute,
		// Optionally provide a customized RetryPolicy.
		// Temporal retries failed Activities by default.
		RetryPolicy: retrypolicy,
	}

	// Apply the options.
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	chatStreamOptions := persisted_ai.ChatStreamOptions{
		WorkspaceId:  "<workspace_id>",   // Replace with valid value
		FlowId:       "<flow_id>",        // Replace with current flow ID
		FlowActionId: "<flow_action_id>", // Replace with action ID
		ToolChatOptions: llm.ToolChatOptions{
			Secrets: secret_manager.SecretManagerContainer{
				SecretManager: &secret_manager.EnvSecretManager{},
			},
			Params: llm.ToolChatParams{
				Messages: []llm.ChatMessage{
					{
						Role:    llm.ChatMessageRoleUser,
						Content: "That's cool.",
					},
				},
				ToolChoice: llm.ToolChoice{
					Type: llm.ToolChoiceTypeRequired,
				},
				Tools: []*llm.Tool{
					{
						Name:        "describe_sentiment",
						Description: "This tool is used to describe the sentiment of the user's message.",
						Parameters:  anthropic.GenerateInputSchema(&SentimentRequest{}),
					},
				},
				ModelConfig: common.ModelConfig{
					Provider: string(llm.AnthropicToolChatProviderType),
				},
			},
		},
	}

	var chatResponse llm.ChatMessageResponse
	la := persisted_ai.LlmActivities{Streamer: redis.NewTestRedisStreamer()}
	err := workflow.ExecuteActivity(ctx, la.ChatStream, chatStreamOptions).Get(ctx, &chatResponse)
	if err != nil {
		return "", err
	}

	//if !(chatResponse.StopReason == string(openai.FinishReasonToolCalls) || chatResponse.StopReason == string(openai.FinishReasonStop)) {
	//	return "", errors.New("Expected finish reason to be stop or tool calls")
	//}

	jsonStr := chatResponse.ToolCalls[0].Arguments
	var result map[string]string
	err = json.Unmarshal([]byte(jsonStr), &result)
	if err != nil {
		return "", fmt.Errorf("Failed to unmarshall json to map[string]string: %v", err)
	}

	if result["sentiment"] != "positive" {
		return "", fmt.Errorf("Expected sentiment to be positive, but it wasn't. Result: %v", result)
	}

	return jsonStr, nil
}
