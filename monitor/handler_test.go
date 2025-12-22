package monitor

import (
	"net/http"
	"testing"

	"binaryDeploy/processmanager"
)

func TestNewHandler(t *testing.T) {
	pm := processmanager.NewProcessManager()
	config := &ServerConfig{
		Port:            "8080",
		TargetRepoURL:   "https://github.com/test/repo.git",
		AllowedBranches: []string{"main", "develop"},
	}

	handler := NewHandler(pm, config)

	if handler.processManager != pm {
		t.Error("ProcessManager not set correctly")
	}

	if handler.serverConfig.Port != "8080" {
		t.Error("Server config not set correctly")
	}

	if len(handler.serverConfig.AllowedBranches) != 2 {
		t.Error("Allowed branches not set correctly")
	}
}

func TestRegisterRoutes(t *testing.T) {
	pm := processmanager.NewProcessManager()
	config := &ServerConfig{Port: "8080"}
	handler := NewHandler(pm, config)

	// Create a test mux and register routes
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Test that routes are registered (basic smoke test)
	// In a real test, you'd make actual HTTP requests to verify behavior
	// For now, just ensure no panics occur during registration
}
