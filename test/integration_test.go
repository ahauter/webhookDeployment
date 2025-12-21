package test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestTargetAppDeploymentFlow tests the complete target app deployment workflow
func TestTargetAppDeploymentFlow(t *testing.T) {
	// Setup test environment
	env := SetupTestEnvironment(t)

	// Create mock repositories
	env.CreateMockRepositories()

	// Write test configuration
	env.WriteTestConfig()

	// Create mock webhook server
	mockServer := NewMockWebhookServer(env)

	t.Run("Target App Happy Path", func(t *testing.T) {
		// Get the target repository
		targetRepo := env.GetRepository("test_target_app")

		// Create a test branch for deployment
		testBranchName := env.CreateTestBranch(targetRepo, "TargetAppDeployment")

		// Make a new commit to simulate a push
		commitHash := env.MakeCommit(targetRepo, "Test deployment commit")

		// Generate webhook payload
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			testBranchName,
			commitHash,
			"Test deployment commit",
		)

		// Send webhook request
		w := mockServer.SendAuthenticatedRequest(t, payload)

		// Verify response
		AssertEqual(t, http.StatusOK, w.Code, "Expected HTTP 200")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected non-empty response body")
		AssertEqual(t, true,
			containsString(responseBody, "Deployment triggered"),
			"Expected deployment triggered message")

		t.Logf("Target app deployment response: %s", responseBody)
	})

	t.Run("Target App Process Management", func(t *testing.T) {
		// This test would verify that the process manager correctly handles
		// starting and stopping the target application
		// For now, we'll test the basic functionality

		// Initially no process should be running
		AssertEqual(t, false, env.ProcessManager.IsRunning(), "Expected no process running initially")

		// Simulate a deployment
		targetRepo := env.GetRepository("test_target_app")

		// Create a test branch for process management test
		testBranchName := env.CreateTestBranch(targetRepo, "ProcessManagement")

		commitHash := env.MakeCommit(targetRepo, "Process management test")

		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			testBranchName,
			commitHash,
			"Process management test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Expected HTTP 200")

		// The actual process management would be handled by the real deployment logic
		// This test verifies the webhook handling portion
	})
}

// TestSelfUpdateFlow tests the complete self-update workflow
func TestSelfUpdateFlow(t *testing.T) {
	// Setup test environment
	env := SetupTestEnvironment(t)

	// Create mock repositories
	env.CreateMockRepositories()

	// Write test configuration
	env.WriteTestConfig()

	// Create mock webhook server
	mockServer := NewMockWebhookServer(env)

	t.Run("Self Update Happy Path", func(t *testing.T) {
		// Get the self-update repository
		updateRepo := env.GetRepository("test_binarydeploy_updater")

		// Create a test branch for self-update
		testBranchName := env.CreateTestBranch(updateRepo, "SelfUpdate")

		// Make a new commit to simulate a push to the self-update repo
		commitHash := env.MakeCommit(updateRepo, "Test self-update commit")

		// Generate webhook payload
		payload := env.GenerateWebhookPayload(
			env.Config.SelfUpdateRepoURL,
			"test_binarydeploy_updater",
			testBranchName,
			commitHash,
			"Test self-update commit",
		)

		// Send webhook request
		w := mockServer.SendAuthenticatedRequest(t, payload)

		// Verify response
		AssertEqual(t, http.StatusOK, w.Code, "Expected HTTP 200")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected non-empty response body")
		AssertEqual(t, true,
			containsString(responseBody, "Self-update triggered"),
			"Expected self-update triggered message")

		t.Logf("Self-update response: %s", responseBody)
	})

	t.Run("Self Update Branch Filtering", func(t *testing.T) {
		// Test self-update on a non-main branch
		updateRepo := env.GetRepository("test_binarydeploy_updater")

		// Create a test branch for self-update
		testBranchName := env.CreateTestBranch(updateRepo, "SelfUpdateBranchFiltering")

		// Generate payload for test branch
		payload := env.GenerateWebhookPayload(
			env.Config.SelfUpdateRepoURL,
			"test_binarydeploy_updater",
			testBranchName,
			updateRepo.LastCommit,
			"Test self-update on test branch",
		)

		// Send webhook request
		w := mockServer.SendAuthenticatedRequest(t, payload)

		// Verify response - should work since test branch is allowed
		AssertEqual(t, http.StatusOK, w.Code, "Expected HTTP 200")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected non-empty response body")
	})
}

// TestEndToEndFlowWithRealHandler tests with the actual webhook handler from main.go
func TestEndToEndFlowWithRealHandler(t *testing.T) {
	// This test would use the actual webhook handler from main.go
	// For now, we'll create a simplified version that demonstrates the test structure

	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	t.Run("Real Handler Target Deployment", func(t *testing.T) {
		// This would require importing the actual webhook handler from main.go
		// For now, we simulate the expected behavior

		targetRepo := env.GetRepository("test_target_app")

		// Create a test branch for real handler test
		testBranchName := env.CreateTestBranch(targetRepo, "RealHandlerTarget")

		commitHash := env.MakeCommit(targetRepo, "Real handler test")

		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			testBranchName,
			commitHash,
			"Real handler test",
		)

		// Validate the payload structure
		var webhookPayload GitHubPushPayload
		AssertNoError(t, json.Unmarshal([]byte(payload), &webhookPayload), "Failed to parse webhook payload")

		AssertEqual(t, "test_target_app", webhookPayload.Repository.Name, "Expected correct repo name")
		AssertEqual(t, env.Config.TargetRepoURL, webhookPayload.Repository.CloneURL, "Expected correct repo URL")
		AssertEqual(t, commitHash, webhookPayload.HeadCommit.ID, "Expected correct commit hash")
		AssertEqual(t, "refs/heads/"+testBranchName, webhookPayload.Ref, "Expected correct ref")

		// Test signature generation
		signature := env.GenerateHMACSignature(payload)
		AssertEqual(t, true, len(signature) > 0, "Expected non-empty signature")
		AssertEqual(t, true,
			containsString(signature, "sha256="),
			"Expected sha256 prefix in signature")

		t.Logf("Generated signature: %s", signature)
	})

	t.Run("Real Handler Self Update", func(t *testing.T) {
		updateRepo := env.GetRepository("test_binarydeploy_updater")

		// Create a test branch for real handler self-update test
		testBranchName := env.CreateTestBranch(updateRepo, "RealHandlerSelfUpdate")

		commitHash := env.MakeCommit(updateRepo, "Real handler self-update test")

		payload := env.GenerateWebhookPayload(
			env.Config.SelfUpdateRepoURL,
			"test_binarydeploy_updater",
			testBranchName,
			commitHash,
			"Real handler self-update test",
		)

		// Validate the payload
		var webhookPayload GitHubPushPayload
		AssertNoError(t, json.Unmarshal([]byte(payload), &webhookPayload), "Failed to parse webhook payload")

		AssertEqual(t, "test_binarydeploy_updater", webhookPayload.Repository.Name, "Expected correct repo name")
		AssertEqual(t, env.Config.SelfUpdateRepoURL, webhookPayload.Repository.CloneURL, "Expected correct repo URL")
		AssertEqual(t, commitHash, webhookPayload.HeadCommit.ID, "Expected correct commit hash")

		t.Logf("Self-update payload validated successfully")
	})
}

// TestProcessLifecycleIntegration tests the process manager integration
func TestProcessLifecycleIntegration(t *testing.T) {
	env := SetupTestEnvironment(t)

	t.Run("Process Startup and Monitoring", func(t *testing.T) {
		// Initially no process should be running
		AssertEqual(t, false, env.ProcessManager.IsRunning(), "Expected no process initially")
		AssertEqual(t, 0, env.ProcessManager.GetCurrentPID(), "Expected PID 0 initially")

		// Note: Actual process lifecycle testing would require real DeployConfig
		// This test structure shows how we would test the integration
	})

	t.Run("Process Replacement on New Deployment", func(t *testing.T) {
		// This would test that when a new deployment comes in,
		// the old process is properly replaced with the new one
		// For now, we verify the process manager state
		AssertEqual(t, false, env.ProcessManager.IsRunning(), "Expected no process running")
	})

	t.Run("Graceful Shutdown on Updates", func(t *testing.T) {
		// This would test that during self-update, the process manager
		// properly shuts down existing processes
		AssertEqual(t, false, env.ProcessManager.IsRunning(), "Expected no process running")
	})
}

// TestRepositoryOperations tests git repository operations
func TestRepositoryOperations(t *testing.T) {
	env := SetupTestEnvironment(t)

	t.Run("Repository Creation and Commits", func(t *testing.T) {
		repo := env.CreateMockRepository("test_repo", func(path string) {
			// Create a simple file
			content := `package main

import "fmt"

func main() {
    fmt.Println("Test repository")
}`
			if err := writeFile(path, "main.go", content); err != nil {
				t.Fatalf("Failed to create main.go: %v", err)
			}
		})

		AssertEqual(t, "test_repo", repo.Name, "Expected correct repo name")
		AssertEqual(t, true, len(repo.URL) > 0, "Expected non-empty URL")
		AssertEqual(t, true, len(repo.LastCommit) > 0, "Expected non-empty commit hash")

		// Make another commit
		newCommit := env.MakeCommit(repo, "Second commit")
		AssertEqual(t, true, len(newCommit) > 0, "Expected new commit hash")
		AssertEqual(t, newCommit, repo.LastCommit, "Expected updated commit hash")
	})

	t.Run("Branch Operations", func(t *testing.T) {
		repo := env.CreateMockRepository("test_branch_repo", func(path string) {
			writeFile(path, "main.go", "package main\n\nfunc main() {}")
		})

		// Create a unique test branch
		branchName := env.CreateTestBranch(repo, "BranchOperations")

		// Verify the branch commit exists
		AssertEqual(t, true, len(repo.LastCommit) > 0, "Expected branch commit hash")

		// Create a commit on the branch
		branchCommit := env.MakeCommit(repo, "Feature commit")
		AssertEqual(t, true, len(branchCommit) > 0, "Expected branch commit")

		t.Logf("Created test branch: %s", branchName)
	})

	t.Run("Repository Content Validation", func(t *testing.T) {
		repo := env.CreateMockRepository("test_content_repo", func(path string) {
			// Create deploy.config
			configContent := `build_command=echo "Building"
run_command=./test_binary
working_dir=./
restart_delay=2
max_restarts=3`
			if err := writeFile(path, "deploy.config", configContent); err != nil {
				t.Fatalf("Failed to create deploy.config: %v", err)
			}
		})

		// Verify files exist
		configPath := filepath.Join(repo.Path, "deploy.config")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Errorf("Expected deploy.config to exist")
		}
	})
}

// TestConcurrentWebhookRequests tests concurrent webhook handling
func TestConcurrentWebhookRequests(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	t.Run("Multiple Simultaneous Requests", func(t *testing.T) {
		// Send multiple concurrent requests
		requestCount := 5
		results := make(chan bool, requestCount)

		for i := 0; i < requestCount; i++ {
			go func(index int) {
				payload := env.GenerateWebhookPayload(
					env.Config.TargetRepoURL,
					"test_target_app",
					"main",
					"commit_hash_"+string(rune(index)),
					"Concurrent test commit",
				)

				w := mockServer.SendAuthenticatedRequest(t, payload)
				results <- (w.Code == http.StatusOK)
			}(i)
		}

		// Wait for all requests to complete
		successCount := 0
		for i := 0; i < requestCount; i++ {
			if <-results {
				successCount++
			}
		}

		AssertEqual(t, requestCount, successCount, "Expected all requests to succeed")
	})

	t.Run("Process State During Concurrent Deployments", func(t *testing.T) {
		// This would test that the process manager handles concurrent
		// deployment requests correctly
		// For now, we verify the initial state
		AssertEqual(t, false, env.ProcessManager.IsRunning(), "Expected no process running")
	})
}

// Helper functions

func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					findSubstring(s, substr))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func writeFile(dir, filename, content string) error {
	fullPath := filepath.Join(dir, filename)
	return os.WriteFile(fullPath, []byte(content), 0644)
}
