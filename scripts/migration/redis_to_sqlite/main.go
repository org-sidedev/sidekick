package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"

	"sidekick/common"
	"sidekick/domain"
	redisStorage "sidekick/srv/redis"
	"sidekick/srv/sqlite"
)

func main() {
	ctx := context.Background()

	// Initialize Redis client
	redisClient := redisStorage.NewStorage()

	// Initialize SQLite client
	sidekickDataHome, err := common.GetSidekickDataHome()
	if err != nil {
		log.Fatalf("Failed to get Sidekick data home: %v", err)
	}
	sqliteDbPath := filepath.Join(sidekickDataHome, "sidekick.db")
	sqliteClient, err := initializeSQLiteStorage(sqliteDbPath)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite storage: %v", err)
	}

	// Retrieve all workspace IDs from Redis
	workspaceIDs, err := getAllWorkspaceIDs(ctx, redisClient)
	if err != nil {
		log.Fatalf("Failed to retrieve workspace IDs: %v", err)
	}

	// Initialize counters
	counters := make(map[string]int)

	// Iterate through workspace IDs and migrate data
	for _, workspaceID := range workspaceIDs {
		fmt.Printf("Processing workspace: %s\n", workspaceID)

		err = migrateWorkspace(ctx, redisClient, sqliteClient, workspaceID, &counters)
		if err != nil {
			log.Fatalf("Failed to migrate workspace %s: %v", workspaceID, err)
		}
	}

	// Print migration results
	fmt.Println("\nMigration completed. Results:")
	for dataType, count := range counters {
		fmt.Printf("%s: %d\n", dataType, count)
	}
}

func initializeSQLiteStorage(dbPath string) (*sqlite.Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Set journal mode to WAL
	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		return nil, fmt.Errorf("failed to set WAL journal mode: %w", err)
	}

	kvDb, err := sql.Open("sqlite3", dbPath+".kv")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite KV database: %w", err)
	}

	return sqlite.NewStorage(db, kvDb), nil
}

func getAllWorkspaceIDs(ctx context.Context, redisClient *redisStorage.Storage) ([]string, error) {
	workspaces, err := redisClient.GetAllWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get all workspaces: %w", err)
	}

	workspaceIDs := make([]string, len(workspaces))
	for i, workspace := range workspaces {
		workspaceIDs[i] = workspace.Id
	}

	return workspaceIDs, nil
}

func migrateWorkspace(ctx context.Context, redisClient *redisStorage.Storage, sqliteClient *sqlite.Storage, workspaceID string, counters *map[string]int) error {
	// Migrate workspace
	workspace, err := redisClient.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to get workspace %s from Redis: %w", workspaceID, err)
	}

	err = sqliteClient.PersistWorkspace(ctx, workspace)
	if err != nil {
		return fmt.Errorf("failed to persist workspace %s to SQLite: %w", workspaceID, err)
	}

	(*counters)["workspaces"]++
	fmt.Printf("Migrated workspace: %s\n", workspaceID)

	// Migrate workspace config
	config, err := redisClient.GetWorkspaceConfig(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to get workspace config for %s from Redis: %w", workspaceID, err)
	}

	err = sqliteClient.PersistWorkspaceConfig(ctx, workspaceID, config)
	if err != nil {
		return fmt.Errorf("failed to persist workspace config for %s to SQLite: %w", workspaceID, err)
	}

	(*counters)["workspace_configs"]++
	fmt.Printf("Migrated workspace config: %s\n", workspaceID)

	// Migrate Tasks, Flows, Subflows, and FlowActions
	err = migrateTasksAndFlows(ctx, redisClient, sqliteClient, workspaceID, counters)
	if err != nil {
		return fmt.Errorf("failed to migrate tasks and flows for workspace %s: %w", workspaceID, err)
	}

	return nil
}

func migrateTasksAndFlows(ctx context.Context, redisClient *redisStorage.Storage, sqliteClient *sqlite.Storage, workspaceID string, counters *map[string]int) error {
	// Retrieve all tasks for the workspace from Redis
	tasks, err := redisClient.GetTasks(ctx, workspaceID, domain.AllTaskStatuses)
	if err != nil {
		return fmt.Errorf("failed to get tasks for workspace %s from Redis: %w", workspaceID, err)
	}

	for _, task := range tasks {
		err := migrateTask(ctx, redisClient, sqliteClient, workspaceID, task, counters)
		if err != nil {
			return err
		}
	}

	return nil
}

func migrateTask(ctx context.Context, redisClient *redisStorage.Storage, sqliteClient *sqlite.Storage, workspaceID string, task domain.Task, counters *map[string]int) error {
	// Migrate task
	err := sqliteClient.PersistTask(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to persist task %s to SQLite: %w", task.Id, err)
	}
	(*counters)["tasks"]++
	fmt.Printf("Migrated task: %s\n", task.Id)

	// Migrate associated flows
	err = migrateFlowsForTask(ctx, redisClient, sqliteClient, workspaceID, task.Id, counters)
	if err != nil {
		return err
	}

	return nil
}

func migrateFlowsForTask(ctx context.Context, redisClient *redisStorage.Storage, sqliteClient *sqlite.Storage, workspaceID, taskID string, counters *map[string]int) error {
	flows, err := redisClient.GetFlowsForTask(ctx, workspaceID, taskID)
	if err != nil {
		return fmt.Errorf("failed to get flows for task %s from Redis: %w", taskID, err)
	}

	for _, flow := range flows {
		err = sqliteClient.PersistFlow(ctx, flow)
		if err != nil {
			return fmt.Errorf("failed to persist flow %s for task %s to SQLite: %w", flow.Id, taskID, err)
		}
		(*counters)["flows"]++
		fmt.Printf("Migrated flow %s for task: %s\n", flow.Id, taskID)

		err = migrateFlowActionsAndSubflows(ctx, redisClient, sqliteClient, workspaceID, flow.Id, counters)
		if err != nil {
			return err
		}
	}

	return nil
}

func migrateFlowActionsAndSubflows(ctx context.Context, redisClient *redisStorage.Storage, sqliteClient *sqlite.Storage, workspaceID, flowID string, counters *map[string]int) error {
	// Migrate flow actions
	actions, err := redisClient.GetFlowActions(ctx, workspaceID, flowID)
	if err != nil {
		return fmt.Errorf("failed to get flow actions for flow %s from Redis: %w", flowID, err)
	}

	for _, action := range actions {
		err = sqliteClient.PersistFlowAction(ctx, action)
		if err != nil {
			return fmt.Errorf("failed to persist flow action %s to SQLite: %w", action.Id, err)
		}
		(*counters)["flow_actions"]++
	}
	fmt.Printf("Migrated %d flow actions for flow: %s\n", len(actions), flowID)

	// Migrate subflows
	subflows, err := redisClient.GetSubflows(ctx, workspaceID, flowID)
	if err != nil {
		return fmt.Errorf("failed to get subflows for flow %s from Redis: %w", flowID, err)
	}

	for _, subflow := range subflows {
		err = sqliteClient.PersistSubflow(ctx, subflow)
		if err != nil {
			return fmt.Errorf("failed to persist subflow %s to SQLite: %w", subflow.Id, err)
		}
		(*counters)["subflows"]++
	}
	fmt.Printf("Migrated %d subflows for flow: %s\n", len(subflows), flowID)

	return nil
}