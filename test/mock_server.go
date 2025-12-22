package test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// MockWebhookServer provides a mock webhook server for testing
type MockWebhookServer struct {
	Handler http.Handler
	Config  *TestConfig
}

// NewMockWebhookServer creates a new mock webhook server
func NewMockWebhookServer(env *TestEnvironment) *MockWebhookServer {
	// We need to create a simple webhook handler that mimics the real one
	// For now, we'll create a basic version that we can test against
	handler := http.NewServeMux()

	server := &MockWebhookServer{
		Handler: handler,
		Config:  env.Config,
	}

	// Setup routes similar to the main application
	server.setupRoutes()

	return server
}

// setupRoutes sets up the webhook routes
func (s *MockWebhookServer) setupRoutes() {
	mux := s.Handler.(*http.ServeMux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Webhook server is running")
	})

	mux.HandleFunc("/webhook", s.mockWebhookHandler)
}

// mockWebhookHandler is a simplified version of the main webhook handler
func (s *MockWebhookServer) mockWebhookHandler(w http.ResponseWriter, r *http.Request) {
	// Log incoming request
	fmt.Printf("Mock webhook: %s %s\n", r.Method, r.URL.Path)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		http.Error(w, "Missing signature", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	// Validate payload is not empty
	if len(body) == 0 {
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	// Validate JSON structure - reject empty objects
	trimmedBody := strings.TrimSpace(string(body))
	if trimmedBody == "{}" {
		http.Error(w, "Invalid JSON payload - empty object", http.StatusBadRequest)
		return
	}

	if !s.verifySignature(body, signature) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var payload GitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Validate required GitHub webhook fields
	if payload.Repository.Name == "" {
		http.Error(w, "Invalid payload - missing repository name", http.StatusBadRequest)
		return
	}
	if payload.Ref == "" {
		http.Error(w, "Invalid payload - missing ref", http.StatusBadRequest)
		return
	}
	if payload.HeadCommit.ID == "" {
		http.Error(w, "Invalid payload - missing commit ID", http.StatusBadRequest)
		return
	}

	branch := extractBranchFromRef(payload.Ref)
	if !s.isAllowedBranch(branch) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Branch %s is not configured for auto-deployment", branch)
		return
	}

	// Check repository and route accordingly
	if payload.Repository.CloneURL == s.Config.SelfUpdateRepoURL {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Self-update triggered for %s", payload.Repository.Name)
	} else if payload.Repository.CloneURL == s.Config.TargetRepoURL {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Deployment triggered for %s", payload.Repository.Name)
	} else {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Repository not configured for deployment")
	}
}

// verifySignature verifies the HMAC signature
func (s *MockWebhookServer) verifySignature(body []byte, signature string) bool {
	if s.Config.Secret == "" {
		return true
	}

	expectedSig := "sha256=" + computeHMAC(body, s.Config.Secret)
	return hmacEqual([]byte(signature), []byte(expectedSig))
}

// isAllowedBranch checks if the branch is allowed
func (s *MockWebhookServer) isAllowedBranch(branch string) bool {
	if len(s.Config.AllowedBranches) == 0 {
		return true
	}
	for _, allowed := range s.Config.AllowedBranches {
		// Support wildcard patterns like "test-*"
		if strings.HasSuffix(allowed, "*") {
			prefix := strings.TrimSuffix(allowed, "*")
			if strings.HasPrefix(branch, prefix) {
				return true
			}
		} else if branch == allowed {
			return true
		}
	}
	return false
}

// computeHMAC computes HMAC-SHA256
func computeHMAC(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// hmacEqual securely compares two byte slices
func hmacEqual(x, y []byte) bool {
	if len(x) != len(y) {
		return false
	}
	var result byte
	for i := range x {
		result |= x[i] ^ y[i]
	}
	return result == 0
}

// extractBranchFromRef extracts branch name from git ref
func extractBranchFromRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}

// SendAuthenticatedRequest sends an authenticated webhook request
func (s *MockWebhookServer) SendAuthenticatedRequest(t *testing.T, payload string) *httptest.ResponseRecorder {
	t.Helper()

	signature := "sha256=" + computeHMAC([]byte(payload), s.Config.Secret)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signature)

	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, req)

	return w
}

// SendUnauthenticatedRequest sends a webhook request without proper authentication
func (s *MockWebhookServer) SendUnauthenticatedRequest(t *testing.T, payload string, missingSignature bool) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	if !missingSignature {
		req.Header.Set("X-Hub-Signature-256", "sha256=invalid_signature")
	}

	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, req)

	return w
}

// SendRequestWithMethod sends a request with a specific HTTP method
func (s *MockWebhookServer) SendRequestWithMethod(t *testing.T, method, payload string) *httptest.ResponseRecorder {
	t.Helper()

	signature := "sha256=" + computeHMAC([]byte(payload), s.Config.Secret)

	req := httptest.NewRequest(method, "/webhook", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signature)

	w := httptest.NewRecorder()
	s.Handler.ServeHTTP(w, req)

	return w
}
