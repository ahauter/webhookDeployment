package test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWebhookLogic tests webhook business logic
func TestWebhookLogic(t *testing.T) {
	// Setup test environment
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	t.Run("Branch Filtering - Authorized Branches", func(t *testing.T) {
		// Test main branch (should be allowed)
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"main_branch_commit",
			"Main branch test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Main branch should be allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Deployment triggered"),
			"Expected deployment triggered for main branch")

		// Create a test branch for this test
		testTargetRepo := env.GetRepository("test_target_app")
		testBranchName := env.CreateTestBranch(testTargetRepo, "BranchFiltering")

		// Test test-branch (should be allowed per config)
		payload = env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			testBranchName,
			"test_branch_commit",
			"Test branch deployment",
		)

		w = mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Test-branch should be allowed")

		responseBody = w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Deployment triggered"),
			"Expected deployment triggered for "+testBranchName)
	})

	t.Run("Branch Filtering - Unauthorized Branches", func(t *testing.T) {
		// Test unauthorized branch
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"unauthorized-branch",
			"unauthorized_commit",
			"Unauthorized branch test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Should return 200 but with rejection message")

		responseBody := w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "not configured for auto-deployment"),
			"Expected branch not configured message")
		t.Logf("Unauthorized branch response: %s", responseBody)
	})

	t.Run("Repository Filtering - Known Repositories", func(t *testing.T) {
		// Test target repository
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"target_repo_commit",
			"Target repository test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Target repository should be recognized")

		responseBody := w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Deployment triggered"),
			"Expected deployment triggered for target repo")

		// Test self-update repository
		payload = env.GenerateWebhookPayload(
			env.Config.SelfUpdateRepoURL,
			"test_binarydeploy_updater",
			"main",
			"self_update_commit",
			"Self update test",
		)

		w = mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Self-update repository should be recognized")

		responseBody = w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Self-update triggered"),
			"Expected self-update triggered")
	})

	t.Run("Repository Filtering - Unknown Repository", func(t *testing.T) {
		// Test unknown repository
		payload := env.GenerateWebhookPayload(
			"file:///unknown/repo",
			"unknown_repo",
			"main",
			"unknown_repo_commit",
			"Unknown repository test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Should return 200 but with rejection message")

		responseBody := w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Repository not configured for deployment"),
			"Expected repository not configured message")
		t.Logf("Unknown repository response: %s", responseBody)
	})

	t.Run("Ref Parsing", func(t *testing.T) {
		// Test different ref formats
		testCases := []struct {
			ref            string
			expectedBranch string
			shouldDeploy   bool
		}{
			{"refs/heads/main", "main", true},
			{"refs/heads/test-branch", "test-branch", true},
			{"refs/heads/unauthorized-branch", "unauthorized-branch", false},
			{"refs/tags/v1.0.0", "tags/v1.0.0", false}, // Tags not branches
			{"main", "main", true},                     // Direct branch name (unlikely but test anyway)
		}

		for _, tc := range testCases {
			payload := env.GenerateWebhookPayload(
				env.Config.TargetRepoURL,
				"test_target_app",
				tc.expectedBranch,
				"ref_test_commit",
				"Ref parsing test",
			)

			// Override the ref in the payload
			modifiedPayload := modifyRefInPayload(payload, tc.ref)

			w := mockServer.SendAuthenticatedRequest(t, modifiedPayload)
			AssertEqual(t, http.StatusOK, w.Code, "Should return 200 for ref: "+tc.ref)

			responseBody := w.Body.String()

			if tc.shouldDeploy {
				AssertEqual(t, true,
					containsString(responseBody, "Deployment triggered"),
					"Expected deployment for ref: "+tc.ref)
			} else {
				AssertEqual(t, true,
					containsString(responseBody, "not configured"),
					"Expected rejection for ref: "+tc.ref)
			}
		}
	})
}

// TestHTTPMethodValidation tests HTTP method handling
func TestHTTPMethodValidation(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	payload := env.GenerateWebhookPayload(
		env.Config.TargetRepoURL,
		"test_target_app",
		"main",
		"method_test_commit",
		"HTTP method test",
	)

	t.Run("POST Method - Allowed", func(t *testing.T) {
		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "POST method should be allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected response body")
		t.Logf("POST method response: %s", responseBody)
	})

	t.Run("GET Method - Not Allowed", func(t *testing.T) {
		w := mockServer.SendRequestWithMethod(t, "GET", payload)
		AssertEqual(t, http.StatusMethodNotAllowed, w.Code, "GET method should be not allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("GET method response: %s", responseBody)
	})

	t.Run("PUT Method - Not Allowed", func(t *testing.T) {
		w := mockServer.SendRequestWithMethod(t, "PUT", payload)
		AssertEqual(t, http.StatusMethodNotAllowed, w.Code, "PUT method should be not allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("PUT method response: %s", responseBody)
	})

	t.Run("DELETE Method - Not Allowed", func(t *testing.T) {
		w := mockServer.SendRequestWithMethod(t, "DELETE", payload)
		AssertEqual(t, http.StatusMethodNotAllowed, w.Code, "DELETE method should be not allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("DELETE method response: %s", responseBody)
	})

	t.Run("PATCH Method - Not Allowed", func(t *testing.T) {
		w := mockServer.SendRequestWithMethod(t, "PATCH", payload)
		AssertEqual(t, http.StatusMethodNotAllowed, w.Code, "PATCH method should be not allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("PATCH method response: %s", responseBody)
	})

	t.Run("HEAD Method - Not Allowed", func(t *testing.T) {
		w := mockServer.SendRequestWithMethod(t, "HEAD", payload)
		AssertEqual(t, http.StatusMethodNotAllowed, w.Code, "HEAD method should be not allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("HEAD method response: %s", responseBody)
	})

	t.Run("OPTIONS Method - Not Allowed", func(t *testing.T) {
		w := mockServer.SendRequestWithMethod(t, "OPTIONS", payload)
		AssertEqual(t, http.StatusMethodNotAllowed, w.Code, "OPTIONS method should be not allowed")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("OPTIONS method response: %s", responseBody)
	})
}

// TestRootEndpoint tests the root endpoint
func TestRootEndpoint(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	t.Run("Root Endpoint GET", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusOK, w.Code, "Root endpoint should return 200")

		responseBody := w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Webhook server is running"),
			"Expected server running message")
		t.Logf("Root endpoint response: %s", responseBody)
	})

	t.Run("Root Endpoint POST", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		w := httptest.NewRecorder()

		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusOK, w.Code, "Root endpoint POST should return 200")

		responseBody := w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Webhook server is running"),
			"Expected server running message")
	})

	t.Run("Root Endpoint HEAD", func(t *testing.T) {
		req := httptest.NewRequest("HEAD", "/", nil)
		w := httptest.NewRecorder()

		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusOK, w.Code, "Root endpoint HEAD should return 200")
	})
}

// TestInvalidEndpoints tests handling of invalid endpoints
func TestInvalidEndpoints(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	t.Run("Invalid Endpoint with POST", func(t *testing.T) {
		payload := `{"test": "data"}`
		signature := "sha256=" + computeHMAC([]byte(payload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/invalid", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusNotFound, w.Code, "Invalid endpoint should return 404")
		t.Logf("Invalid endpoint response: %s", w.Body.String())
	})

	t.Run("Invalid Endpoint with GET", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/invalid", nil)
		w := httptest.NewRecorder()

		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusNotFound, w.Code, "Invalid endpoint should return 404")
	})

	t.Run("Path Traversal Attempt", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/../etc/passwd", nil)
		w := httptest.NewRecorder()

		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusNotFound, w.Code, "Path traversal should return 404")
	})
}

// TestRequestHeaders tests various header combinations
func TestRequestHeaders(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	payload := env.GenerateWebhookPayload(
		env.Config.TargetRepoURL,
		"test_target_app",
		"main",
		"header_test_commit",
		"Header test",
	)

	t.Run("Standard GitHub Headers", func(t *testing.T) {
		signature := "sha256=" + computeHMAC([]byte(payload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)
		req.Header.Set("X-GitHub-Delivery", "12345678-1234-1234-1234-123456789012")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("User-Agent", "GitHub-Hookshot/abc123")

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusOK, w.Code, "Standard GitHub headers should be accepted")
		t.Logf("GitHub headers response: %s", w.Body.String())
	})

	t.Run("Missing X-Hub-Signature-256 Header", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		// Missing X-Hub-Signature-256 header

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusUnauthorized, w.Code, "Missing signature header should be rejected")
	})

	t.Run("Alternative Signature Header", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature", "sha256="+computeHMAC([]byte(payload), env.Config.Secret))
		// Note: using X-Hub-Signature instead of X-Hub-Signature-256

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusUnauthorized, w.Code, "Wrong signature header should be rejected")
		t.Logf("Alternative signature header response: %s", w.Body.String())
	})

	t.Run("Extra Headers", func(t *testing.T) {
		signature := "sha256=" + computeHMAC([]byte(payload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)
		req.Header.Set("X-Custom-Header", "custom-value")
		req.Header.Set("Authorization", "Bearer token123")
		req.Header.Set("X-Forwarded-For", "192.168.1.1")

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusOK, w.Code, "Extra headers should be ignored")
		t.Logf("Extra headers response: %s", w.Body.String())
	})
}

// TestConfigurationScenarios tests various configuration scenarios
func TestConfigurationScenarios(t *testing.T) {
	t.Run("Empty Allowed Branches Config", func(t *testing.T) {
		env := SetupTestEnvironment(t)
		env.Config.AllowedBranches = []string{} // Empty allowed branches
		env.CreateMockRepositories()
		env.WriteTestConfig()

		mockServer := NewMockWebhookServer(env)

		// Any branch should be allowed when allowed branches is empty
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"any-branch-name",
			"empty_config_commit",
			"Empty allowed branches test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Any branch should be allowed when config is empty")

		responseBody := w.Body.String()
		AssertEqual(t, true,
			containsString(responseBody, "Deployment triggered"),
			"Expected deployment triggered for any branch")
	})

	t.Run("No Webhook Secret Config", func(t *testing.T) {
		env := SetupTestEnvironment(t)
		env.Config.Secret = "" // No secret
		env.CreateMockRepositories()
		env.WriteTestConfig()

		mockServer := NewMockWebhookServer(env)

		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"no_secret_commit",
			"No secret test",
		)

		// Send without signature (should be accepted when no secret is configured)
		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		// No signature header

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Should accept when no secret is configured
		AssertEqual(t, http.StatusOK, w.Code, "Should accept without signature when no secret configured")
		t.Logf("No secret config response: %s", w.Body.String())
	})
}

// Helper function to modify ref in webhook payload
func modifyRefInPayload(payload, newRef string) string {
	// Simple string replacement to change the ref
	// In a real implementation, you'd parse the JSON and modify it properly
	return replaceInJSON(payload, `"ref":`, `"ref": "`+newRef+`"`)
}

// Helper function to replace values in JSON (simplified)
func replaceInJSON(jsonStr, key, value string) string {
	// This is a simplified replacement - in practice you'd use JSON parsing
	// For testing purposes, this should work for our specific cases
	keyStart := findSubstringPosition(jsonStr, key)
	if keyStart == -1 {
		return jsonStr
	}

	valueStart := findSubstringPosition(jsonStr[keyStart:], ":")
	if valueStart == -1 {
		return jsonStr
	}
	valueStart += keyStart

	endPos := findSubstringPosition(jsonStr[valueStart:], ",")
	if endPos == -1 {
		endPos = findSubstringPosition(jsonStr[valueStart:], "}")
	}
	if endPos == -1 {
		return jsonStr
	}
	endPos += valueStart

	return jsonStr[:valueStart+1] + value + jsonStr[endPos:]
}

func findSubstringPosition(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
