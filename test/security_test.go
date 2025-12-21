package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWebhookSecurity tests webhook security features
func TestWebhookSecurity(t *testing.T) {
	// Setup test environment
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	t.Run("Valid Signature Acceptance", func(t *testing.T) {
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"valid_commit_hash",
			"Valid signature test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Valid signature should be accepted")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected response body")
		t.Logf("Valid signature response: %s", responseBody)
	})

	t.Run("Invalid Signature Rejection", func(t *testing.T) {
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"invalid_commit_hash",
			"Invalid signature test",
		)

		// Send with wrong signature
		w := mockServer.SendUnauthenticatedRequest(t, payload, false)
		AssertEqual(t, http.StatusUnauthorized, w.Code, "Invalid signature should be rejected")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("Invalid signature response: %s", responseBody)
	})

	t.Run("Missing Signature Rejection", func(t *testing.T) {
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"missing_signature_commit",
			"Missing signature test",
		)

		// Send without any signature
		w := mockServer.SendUnauthenticatedRequest(t, payload, true)
		AssertEqual(t, http.StatusUnauthorized, w.Code, "Missing signature should be rejected")

		responseBody := w.Body.String()
		AssertEqual(t, true, len(responseBody) > 0, "Expected error message")
		t.Logf("Missing signature response: %s", responseBody)
	})

	t.Run("Signature Tampering Detection", func(t *testing.T) {
		originalPayload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"tamper_test_commit",
			"Original message",
		)

		// Tamper with the payload after generating signature
		tamperedPayload := originalPayload + "tampered_data"

		// Generate signature for original payload (now invalid for tampered payload)
		signature := "sha256=" + computeHMAC([]byte(originalPayload), env.Config.Secret)

		// Create request manually with tampered payload and original signature
		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(tamperedPayload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusUnauthorized, w.Code, "Tampered payload should be rejected")
		t.Logf("Tampering detection response: %s", w.Body.String())
	})

	t.Run("Empty Payload with Signature", func(t *testing.T) {
		emptyPayload := "{}"

		// Generate valid signature for empty payload
		signature := "sha256=" + computeHMAC([]byte(emptyPayload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(emptyPayload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Should get 400 for invalid JSON structure (missing required fields)
		AssertEqual(t, http.StatusBadRequest, w.Code, "Empty/invalid JSON should be rejected")
		t.Logf("Empty payload response: %s", w.Body.String())
	})

	t.Run("Signature Algorithm Validation", func(t *testing.T) {
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"algo_test_commit",
			"Algorithm test",
		)

		// Create signature with wrong algorithm prefix
		signature := "md5=" + computeHMAC([]byte(payload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusUnauthorized, w.Code, "Wrong algorithm should be rejected")
		t.Logf("Algorithm validation response: %s", w.Body.String())
	})
}

// TestJSONValidation tests JSON payload validation
func TestJSONValidation(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	t.Run("Valid JSON Structure", func(t *testing.T) {
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"valid_json_commit",
			"Valid JSON test",
		)

		// Verify payload is valid JSON
		var parsed GitHubPushPayload
		AssertNoError(t, json.Unmarshal([]byte(payload), &parsed), "Payload should be valid JSON")

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Valid JSON should be accepted")
	})

	t.Run("Invalid JSON Syntax", func(t *testing.T) {
		invalidJSON := `{
			"ref": "refs/heads/main",
			"repository": {
				"name": "test",
				"clone_url": "test"
			},
			"head_commit": {
				"id": "abc123",
				"message": "test"
			"missing_closing_brace"
		}`

		// Generate signature for invalid JSON
		signature := "sha256=" + computeHMAC([]byte(invalidJSON), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(invalidJSON)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusBadRequest, w.Code, "Invalid JSON should be rejected")
		t.Logf("Invalid JSON response: %s", w.Body.String())
	})

	t.Run("Malformed JSON with Extra Characters", func(t *testing.T) {
		validJSON := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"malformed_commit",
			"Malformed test",
		)

		// Add extra characters after JSON
		malformedJSON := validJSON + "extra_characters_after_json"

		signature := "sha256=" + computeHMAC([]byte(malformedJSON), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(malformedJSON)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusBadRequest, w.Code, "Malformed JSON should be rejected")
		t.Logf("Malformed JSON response: %s", w.Body.String())
	})

	t.Run("Missing Required Fields", func(t *testing.T) {
		// JSON with missing required fields
		incompleteJSON := `{
			"ref": "refs/heads/main",
			"repository": {
				"name": "test"
			}
		}`

		signature := "sha256=" + computeHMAC([]byte(incompleteJSON), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(incompleteJSON)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Should handle missing fields gracefully
		// This might return 200 but with error message, or 400 depending on implementation
		t.Logf("Missing fields response (code %d): %s", w.Code, w.Body.String())
	})

	t.Run("Incorrect Data Types", func(t *testing.T) {
		// JSON with incorrect data types
		incorrectTypesJSON := `{
			"ref": 123,
			"repository": {
				"name": "test",
				"clone_url": 456
			},
			"head_commit": {
				"id": "abc123",
				"message": ["array", "instead", "of", "string"]
			}
		}`

		signature := "sha256=" + computeHMAC([]byte(incorrectTypesJSON), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(incorrectTypesJSON)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Should handle type errors gracefully
		t.Logf("Incorrect types response (code %d): %s", w.Code, w.Body.String())
	})

	t.Run("Empty JSON Object", func(t *testing.T) {
		emptyJSON := "{}"

		signature := "sha256=" + computeHMAC([]byte(emptyJSON), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(emptyJSON)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		AssertEqual(t, http.StatusBadRequest, w.Code, "Empty JSON object should be rejected")
		t.Logf("Empty JSON object response: %s", w.Body.String())
	})

	t.Run("Null Values", func(t *testing.T) {
		nullValuesJSON := `{
			"ref": null,
			"repository": null,
			"head_commit": null
		}`

		signature := "sha256=" + computeHMAC([]byte(nullValuesJSON), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(nullValuesJSON)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Should handle null values gracefully
		t.Logf("Null values response (code %d): %s", w.Code, w.Body.String())
	})
}

// TestContentTypeValidation tests content-type header validation
func TestContentTypeValidation(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	payload := env.GenerateWebhookPayload(
		env.Config.TargetRepoURL,
		"test_target_app",
		"main",
		"content_type_commit",
		"Content type test",
	)

	t.Run("Correct Content-Type", func(t *testing.T) {
		// Content-Type: application/json is already tested in other tests
		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Correct content-type should be accepted")
	})

	t.Run("Missing Content-Type", func(t *testing.T) {
		signature := "sha256=" + computeHMAC([]byte(payload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("X-Hub-Signature-256", signature)
		// Deliberately not setting Content-Type

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Behavior depends on implementation - might accept or reject
		t.Logf("Missing content-type response (code %d): %s", w.Code, w.Body.String())
	})

	t.Run("Incorrect Content-Type", func(t *testing.T) {
		signature := "sha256=" + computeHMAC([]byte(payload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Behavior depends on implementation - might accept or reject
		t.Logf("Incorrect content-type response (code %d): %s", w.Code, w.Body.String())
	})

	t.Run("Multiple Content-Type Values", func(t *testing.T) {
		signature := "sha256=" + computeHMAC([]byte(payload), env.Config.Secret)

		req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
		req.Header.Set("Content-Type", "application/json, charset=utf-8")
		req.Header.Set("X-Hub-Signature-256", signature)

		w := httptest.NewRecorder()
		mockServer.Handler.ServeHTTP(w, req)

		// Should handle multiple content-type values
		t.Logf("Multiple content-type response (code %d): %s", w.Code, w.Body.String())
	})
}

// TestRequestSizeValidation tests request size limits
func TestRequestSizeValidation(t *testing.T) {
	env := SetupTestEnvironment(t)
	env.CreateMockRepositories()
	env.WriteTestConfig()

	mockServer := NewMockWebhookServer(env)

	t.Run("Normal Size Payload", func(t *testing.T) {
		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"normal_size_commit",
			"Normal size test",
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)
		AssertEqual(t, http.StatusOK, w.Code, "Normal size payload should be accepted")
		t.Logf("Normal payload size: %d bytes", len(payload))
	})

	t.Run("Large Payload", func(t *testing.T) {
		// Create a payload with a very large commit message
		largeMessage := ""
		for i := 0; i < 1000; i++ {
			largeMessage += "This is a very large commit message designed to test size limits. "
		}

		payload := env.GenerateWebhookPayload(
			env.Config.TargetRepoURL,
			"test_target_app",
			"main",
			"large_payload_commit",
			largeMessage,
		)

		w := mockServer.SendAuthenticatedRequest(t, payload)

		// Should handle large payloads (behavior depends on size limits)
		t.Logf("Large payload response (code %d, size: %d bytes): %s",
			w.Code, len(payload), w.Body.String())
	})
}
