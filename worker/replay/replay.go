package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"sidekick/common"
	"sidekick/utils"
	sidekick_worker "sidekick/worker"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	zerologadapter "logur.dev/adapter/zerolog"
	"logur.dev/logur"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	if err := godotenv.Load(); err != nil {
		log.Debug().Err(err).Msg("dot env loading failed")
	}

	// Define flag sets for subcommands
	// store subcommand
	storeCmd := flag.NewFlagSet("store", flag.ExitOnError)
	var storeHostPort, storeWorkflowId, storeSidekickVersion string
	storeCmd.StringVar(&storeHostPort, "hostPort", common.GetTemporalServerHostPort(), "Host and port for the Temporal server (for store command)")
	storeCmd.StringVar(&storeWorkflowId, "id", "", "Workflow ID to store (mandatory for store command)")
	storeCmd.StringVar(&storeSidekickVersion, "sidekick-version", "", "Sidekick version (mandatory for store command)")

	// Default command flags
	var defaultHostPort, defaultWorkflowId string
	flag.StringVar(&defaultHostPort, "hostPort", common.GetTemporalServerHostPort(), "Host and port for the Temporal server, eg localhost:18855 (default command)")
	flag.StringVar(&defaultWorkflowId, "id", "", "Workflow ID to replay (default command, mandatory if no subcommand)")

	// Custom usage messages
	storeCmd.Usage = func() {
		fmt.Println("Usage: replay store -id <workflow_id> -sidekick-version <version> [-hostPort <host:port>]")
		storeCmd.PrintDefaults()
	}
	flag.Usage = func() {
		fmt.Println("Usage: replay [-id <workflow_id>] [-hostPort <host:port>]")
		fmt.Println("Or: replay <subcommand> [options]")
		fmt.Println("Subcommands:")
		fmt.Println("  store          Fetches workflow history and stores it to S3.")
		fmt.Println("\nDefault (if no subcommand is given) is to replay a given flow from the temporal server. This has the following flags:")
		flag.PrintDefaults()
		fmt.Println("\nFor 'store' subcommand usage:\nreplay store --help")
	}

	flag.Parse()

	if flag.NArg() > 0 {
		subcommand := flag.Arg(0)
		args := flag.Args()[1:]

		switch subcommand {
		case "store":
			if err := storeCmd.Parse(args); err != nil {
				log.Error().Err(err).Msg("Error parsing 'store' subcommand flags.")
				storeCmd.Usage() // Show specific usage for store
				os.Exit(1)
			}
			if storeWorkflowId == "" {
				log.Error().Msg("Error: -id is required for 'store' subcommand.")
				storeCmd.Usage()
				os.Exit(1)
			}
			if storeSidekickVersion == "" {
				log.Error().Msg("Error: -sidekick-version is required for 'store' subcommand.")
				storeCmd.Usage()
				os.Exit(1)
			}
			log.Info().Msgf("Executing 'store' command: id=%s, hostPort=%s, sidekick-version=%s", storeWorkflowId, storeHostPort, storeSidekickVersion)
			if err := handleStoreCommand(storeWorkflowId, storeHostPort, storeSidekickVersion); err != nil {
				log.Fatal().Err(err).Msg("Store command execution failed.")
			}
			log.Info().Msgf("Store command for workflow %s (version %s) completed successfully.", storeWorkflowId, storeSidekickVersion)
		default:
			log.Error().Msgf("Unknown subcommand: %s", subcommand)
			flag.Usage() // Show global usage
			os.Exit(1)
		}
	} else {
		// Default command (original behavior)
		if defaultWorkflowId == "" {
			log.Error().Msg("Error: -id is required for default replay command (or specify a subcommand).")
			flag.Usage() // Show global usage
			os.Exit(1)
		}

		log.Info().Msgf("Executing default replay: id=%s, hostPort=%s", defaultWorkflowId, defaultHostPort)

		clientOptions := client.Options{
			Logger:   logur.LoggerToKV(zerologadapter.New(log.Logger)),
			HostPort: defaultHostPort,
		}
		c, err := client.Dial(clientOptions)
		if err != nil {
			log.Fatal().Err(err).Msg("Unable to create Temporal client for default replay.")
		}
		defer c.Close()

		if err := ReplayWorkflowLatest(context.Background(), c, defaultWorkflowId); err != nil {
			log.Fatal().Err(err).Msg("Default replay failed.")
		}
	}
}

var s3Region string = "us-east-2"

const replayTestDataFile = "worker/replay/replay_test_data.json"

func handleStoreCommand(workflowId, hostPort, sidekickVersion string) error {
	log.Info().Msgf("Initiating store command for workflow ID: %s, version: %s", workflowId, sidekickVersion)

	ctx := context.Background()

	log.Info().Msgf("Fetching workflow history for Workflow ID: %s using temporal CLI (host: %s)", workflowId, hostPort)
	cmd := exec.CommandContext(ctx, "temporal", "workflow", "show", "--address", hostPort, "--workflow-id", workflowId, "--output", "json")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	jsonData, err := cmd.Output()
	if err != nil {
		errorMsg := fmt.Sprintf("temporal CLI command failed for workflow %s. Stderr: %s", workflowId, stderrBuf.String())
		return fmt.Errorf("%s: %w", errorMsg, err)
	}
	log.Info().Str("workflowId", workflowId).Int("jsonDataSize", len(jsonData)).Msg("Workflow history fetched successfully via CLI")

	// Initialize S3 client
	s3Client, err := utils.NewS3Client(ctx, &s3Region)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}
	log.Info().Msg("S3 client initialized")

	// Construct S3 bucket, key, and metadata
	s3Bucket := "genflow.dev"
	s3Key := fmt.Sprintf("sidekick/replays/%s/%s_events.json", sidekickVersion, workflowId)
	metadata := map[string]string{
		"workflow-id":      workflowId,
		"sidekick-version": sidekickVersion,
	}
	log.Info().Str("bucket", s3Bucket).Str("key", s3Key).Interface("metadata", metadata).Msg("Preparing to upload to S3")

	// Upload JSON to S3
	err = utils.UploadJSONWithMetadata(ctx, s3Client, s3Bucket, s3Key, jsonData, metadata)
	if err != nil {
		return fmt.Errorf("failed to upload workflow history to S3 (bucket: %s, key: %s): %w", s3Bucket, s3Key, err)
	}

	log.Info().Str("bucket", s3Bucket).Str("key", s3Key).Msg("Successfully uploaded workflow history to S3")

	// Update the test data file with this workflow
	var testData map[string][]string
	if utils.FileExists(replayTestDataFile) {
		testDataBytes, err := os.ReadFile(replayTestDataFile)
		if err != nil {
			return fmt.Errorf("failed to read test data file: %w", err)
		}
		if err := json.Unmarshal(testDataBytes, &testData); err != nil {
			return fmt.Errorf("failed to parse test data file: %w", err)
		}
	} else {
		testData = make(map[string][]string)
	}

	// Add workflowId to the version's list if not already present
	workflowIds := testData[sidekickVersion]
	found := false
	for _, id := range workflowIds {
		if id == workflowId {
			found = true
			break
		}
	}
	if !found {
		testData[sidekickVersion] = append(workflowIds, workflowId)

		// Write updated data back to file
		updatedData, err := json.MarshalIndent(testData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal updated test data: %w", err)
		}
		if err := os.WriteFile(replayTestDataFile, updatedData, 0644); err != nil {
			return fmt.Errorf("failed to write updated test data file: %w", err)
		}
		log.Info().Str("workflowId", workflowId).Str("version", sidekickVersion).Msg("Added workflow to test data file")
	}

	return nil
}

// getReplayCacheFilePath constructs the full path for a cached workflow history file.
// It ensures the parent directory for the cache file exists.
// The path is <SIDEKICK_CACHE_HOME>/replays/<sidekickVersion>/<workflowId>_events.json.
func getReplayCacheFilePath(sidekickVersion string, workflowId string) (string, error) {
	baseCacheDir, err := common.GetSidekickCacheHome()
	if err != nil {
		return "", fmt.Errorf("failed to get Sidekick cache home: %w", err)
	}

	replayFilePath := filepath.Join(baseCacheDir, "replays", sidekickVersion, fmt.Sprintf("%s_events.json", workflowId))

	replayFileDir := filepath.Dir(replayFilePath)
	err = os.MkdirAll(replayFileDir, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create replay cache directory '%s': %w", replayFileDir, err)
	}

	return replayFilePath, nil
}

// cachedHistoryFile retrieves workflow history json file path, locally cached but backed by s3.
// If the history is not in the cache, it downloads from S3 via HTTPS and updates the cache.
func cachedHistoryFile(ctx context.Context, region string, workflowID string, sidekickVersion string) (string, error) {
	if region == "" {
		return "", fmt.Errorf("region parameter cannot be empty")
	}

	cachePath, err := getReplayCacheFilePath(sidekickVersion, workflowID)
	if err != nil {
		return "", fmt.Errorf("failed to get replay cache file path for %s (version %s): %w", workflowID, sidekickVersion, err)
	}

	// Attempt to read from cache
	_, err = os.Stat(cachePath)
	if err == nil {
		log.Debug().Str("workflowId", workflowID).Str("version", sidekickVersion).Str("cachePath", cachePath).Msg("Workflow history successfully loaded from local cache.")
		return cachePath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read cache file %s for workflow %s (version %s): %w", cachePath, workflowID, sidekickVersion, err)
	} else {
		log.Debug().Str("workflowId", workflowID).Str("version", sidekickVersion).Str("cachePath", cachePath).Msg("Workflow history not found in local cache, attempting HTTPS download.")
	}

	// Download via HTTPS
	s3Bucket := "genflow.dev"
	s3Key := fmt.Sprintf("sidekick/replays/%s/%s_events.json", sidekickVersion, workflowID)
	url := fmt.Sprintf("https://s3.%s.amazonaws.com/%s/%s", region, s3Bucket, s3Key)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download history via HTTPS from %s for workflow %s (version %s): %w", url, workflowID, sidekickVersion, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download history via HTTPS, status %d from %s for workflow %s (version %s)", resp.StatusCode, url, workflowID, sidekickVersion)
	}

	jsonData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read history response from %s for workflow %s (version %s): %w", url, workflowID, sidekickVersion, err)
	}
	log.Debug().Str("workflowId", workflowID).Str("version", sidekickVersion).Str("url", url).Msg("Workflow history downloaded via HTTPS.")

	// Write to cache
	if err := os.WriteFile(cachePath, jsonData, 0644); err != nil {
		log.Error().Err(err).Str("workflowId", workflowID).Str("cachePath", cachePath).Msg("Failed to write downloaded history to cache.")

		// fail: we return the file path here, so if we failed to write, that's
		// an error! even if it wasn't (e.g. if we returned the event json
		// directly), we'd still likely want to fail fast
		return "", err
	} else {
		log.Debug().Str("workflowId", workflowID).Str("version", sidekickVersion).Str("cachePath", cachePath).Msg("Workflow history successfully written to local cache.")
	}

	return cachePath, nil
}

func GetWorkflowHistory(ctx context.Context, client client.Client, id, runID string) (*history.History, error) {
	var hist history.History
	iter := client.GetWorkflowHistory(ctx, id, runID, false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			return nil, err
		}
		hist.Events = append(hist.Events, event)
	}
	return &hist, nil
}

func ReplayWorkflow(ctx context.Context, client client.Client, id, runID string) error {
	hist, err := GetWorkflowHistory(ctx, client, id, runID)
	if err != nil {
		return err
	}
	replayer := worker.NewWorkflowReplayer()
	sidekick_worker.RegisterWorkflows(replayer)
	return replayer.ReplayWorkflowHistory(nil, hist)
}

func ReplayWorkflowLatest(ctx context.Context, client client.Client, id string) error {
	hist, err := GetWorkflowHistory(ctx, client, id, "")
	if err != nil {
		return err
	}
	replayer := worker.NewWorkflowReplayer()
	sidekick_worker.RegisterWorkflows(replayer)
	return replayer.ReplayWorkflowHistory(nil, hist)
}
