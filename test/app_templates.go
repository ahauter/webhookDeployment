package test

import (
	"fmt"
)

// CreateSelfUpdateApp creates a BinaryDeploy application template for self-update testing
func CreateSelfUpdateApp(version string) (map[string]string, error) {
	mainGo := `package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var (
	versionFlag = flag.Bool("version", false, "Show version information")
	helpFlag    = flag.Bool("help", false, "Show help information")
)

func main() {
	flag.Parse()

	if *versionFlag {
		fmt.Printf("binaryDeploy-test version %s\n", "` + version + `")
		return
	}

	if *helpFlag {
		fmt.Println("BinaryDeploy - Webhook server for automated deployments")
		fmt.Println("Usage:")
		fmt.Println("  binaryDeploy [flags]")
		fmt.Println("Flags:")
		fmt.Println("  -version  Show version information")
		fmt.Println("  -help     Show help information")
		return
	}

	fmt.Printf("Starting BinaryDeploy webhook server version %s\n", "` + version + `")
	
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	fmt.Println("Server running, waiting for signals...")
	sig := <-sigChan
	fmt.Printf("Received signal %v, shutting down\n", sig)
}`

	goMod := `module binaryDeploy-test

go 1.21

require (
)

// No external dependencies for test binary
`

	deployConfig := `# Self-Update Deployment Configuration
build_command=go build -o binaryDeploy .
run_command=./binaryDeploy
working_dir=.
environment=test
restart_command=echo "Restarting BinaryDeploy"
`

	return map[string]string{
		"main.go":       mainGo,
		"go.mod":        goMod,
		"deploy.config": deployConfig,
		"README.md":     fmt.Sprintf("# BinaryDeploy Test Application v%s\n\nThis is a test application for self-update testing.", version),
	}, nil
}

// CreateTargetApp creates a simple web application for target deployment testing
func CreateTargetApp(port int, crashOnRequest int) (map[string]string, error) {
	mainGo := fmt.Sprintf(`package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
	"syscall"
)

var (
	requestCount = 0
	crashOn     = %d
	serverPort  = %d
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		
		if crashOn > 0 && requestCount %% crashOn == 0 {
			log.Printf("Crashing on request %%d as configured", requestCount)
			os.Exit(1)
		}
		
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"message\": \"Hello from test application!\", \"port\": %%d, \"request_count\": %%d, \"timestamp\": \"%%s\"}", serverPort, requestCount, time.Now().Format(time.RFC3339))
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"status\": \"healthy\", \"port\": %%d, \"uptime\": \"%%s\"}", serverPort, time.Since(time.Now()).String())
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"status\": \"ready\", \"port\": %%d}", serverPort)
	})

	log.Printf("Starting test application on port %%d", serverPort)
	log.Printf("Crash configured to occur on request %%d", crashOn)
	
	go func() {
		portStr := fmt.Sprintf("%%d", serverPort)
		log.Fatal(http.ListenAndServe(":" + portStr, nil))
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	
	log.Println("Shutting down test application")
}`, crashOnRequest, port)

	// Debug: print the first 500 chars to see template processing
	if len(mainGo) > 500 {
		fmt.Printf("DEBUG: mainGo first 500 chars: %s\n", mainGo[:500])
	} else {
		fmt.Printf("DEBUG: mainGo: %s\n", mainGo)
	}

	goMod := `module target-app-test

go 1.21

require (
)

// No external dependencies for test web server
`

	deployConfig := fmt.Sprintf(`# Target Application Deployment Configuration
build_command=go build -o target-app .
run_command=./target-app
working_dir=.
environment=test
port=%d
restart_delay=5
max_restarts=3
`, port)

	return map[string]string{
		"main.go":       mainGo,
		"go.mod":        goMod,
		"deploy.config": deployConfig,
		"README.md":     fmt.Sprintf("# Target Test Application\n\nSimple web server for testing target deployments on port %d.", port),
	}, nil
}

// UpdateSelfUpdateAppVersion creates an updated version of the self-update app
func UpdateSelfUpdateApp(version string) (map[string]string, error) {
	return CreateSelfUpdateApp(version)
}

// UpdateTargetApp creates an updated version with potentially different behavior
func UpdateTargetApp(port int, crashOnRequest int, addNewEndpoint bool) (map[string]string, error) {
	baseFiles, err := CreateTargetApp(port, crashOnRequest)
	if err != nil {
		return nil, err
	}

	if addNewEndpoint {
		// Add a new endpoint to test the update
		newEndpointCode := `
	http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, ` + "`" + `{
			"version": "2.0.0",
			"features": ["new-endpoint", "updated-behavior"],
			"timestamp": "%s"
		}` + "`" + `, time.Now().Format(time.RFC3339))
	})`

		// Inject the new endpoint handler into the main.go code
		mainGo := baseFiles["main.go"]
		updatedMainGo := mainGo[:len(mainGo)-1] + newEndpointCode + mainGo[len(mainGo)-1:]
		baseFiles["main.go"] = updatedMainGo

		// Update README to reflect the new version
		baseFiles["README.md"] = fmt.Sprintf("# Target Test Application v2.0.0\n\nEnhanced web server for testing target deployments on port %d.\n\nNew features:\n- /version endpoint", port)
	}

	return baseFiles, nil
}

// CreateAppWithDependencies creates a more complex app with dependencies for realistic testing
func CreateAppWithDependencies(port int, includeRedis bool) (map[string]string, error) {
	mainGo := fmt.Sprintf(`package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type Response struct {
	Message   string `+"`json:\"message\"`"+`
	Port      int    `+"`json:\"port\"`"+`
	Timestamp string `+"`json:\"timestamp\"`"+`
	Version   string `+"`json:\"version\"`"+`
}

var (
	requestCount = 0
	serverPort  = %d
	appVersion  = "1.0.0"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		
		response := Response{
			Message:   "Hello from complex test application!",
			Port:      serverPort,
			Timestamp: time.Now().Format(time.RFC3339),
			Version:   appVersion,
		}
		
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"status\": \"healthy\", \"port\": %%d, \"version\": \"%%s\", \"uptime\": \"%%s\"}", serverPort, appVersion, time.Since(time.Now()).String())
	})

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"request_count\": %%d, \"uptime_seconds\": %%d, \"memory_usage\": \"%%s\"}", requestCount, int(time.Since(time.Now()).Seconds()), "N/A")
	})

	log.Printf("Starting complex test application on port %%d", serverPort)
	log.Fatal(http.ListenAndServe(\":%%d\", serverPort))
}`, port)

	goMod := `module complex-app-test

go 1.21

require (
	github.com/gorilla/mux v1.8.0
)

require (
	github.com/gorilla/heartbeat v1.0.1 // indirect
)
`

	deployConfig := fmt.Sprintf(`# Complex Application Deployment Configuration
build_command=go build -o complex-app .
run_command=./complex-app
working_dir=.
environment=test
port=%d
restart_delay=3
max_restarts=5
`, port)

	return map[string]string{
		"main.go":       mainGo,
		"go.mod":        goMod,
		"deploy.config": deployConfig,
		"README.md":     fmt.Sprintf("# Complex Test Application\n\nMore realistic web server with dependencies for testing on port %d.", port),
	}, nil
}

// CreateFailingApp creates an app that fails to build or run for error testing
func CreateFailingApp(failType string) (map[string]string, error) {
	switch failType {
	case "build":
		return map[string]string{
			"main.go": `package main

import "not/a/real/package"

func main() {
	// This will fail to build
	println("This won't compile")
}`,
			"go.mod":        "module failing-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o failing-app .\nrun_command=./failing-app\n",
		}, nil

	case "runtime":
		return map[string]string{
			"main.go": `package main

func main() {
	// This will panic at runtime
	var nilPtr *int
	println(*nilPtr)
}`,
			"go.mod":        "module failing-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o failing-app .\nrun_command=./failing-app\n",
		}, nil

	case "port_conflict":
		return map[string]string{
			"main.go": `package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	// Try to bind to a privileged port (will fail)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	})
	
	log.Fatal(http.ListenAndServe(":80", nil))
}`,
			"go.mod":        "module failing-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o failing-app .\nrun_command=./failing-app\n",
		}, nil

	case "config_missing":
		// Repository without deploy.config file
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Config missing test app")
	})
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod": "module config-missing-app\ngo 1.21\n",
		}, nil

	case "config_empty":
		// Empty deploy.config file
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Config empty test app")
	})
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module config-empty-app\ngo 1.21\n",
			"deploy.config": "",
		}, nil

	case "config_malformed":
		// Invalid deploy.config syntax
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Config malformed test app")
	})
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module config-malformed-app\ngo 1.21\n",
			"deploy.config": "invalid_syntax_here_no_equals_separator",
		}, nil

	case "config_missing_build":
		// deploy.config missing build_command
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Missing build command test app")
	})
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module missing-build-app\ngo 1.21\n",
			"deploy.config": "run_command=./app\n",
		}, nil

	case "config_missing_run":
		// deploy.config missing run_command
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Missing run command test app")
	})
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module missing-run-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o app .\n",
		}, nil

	case "config_invalid_values":
		// deploy.config with invalid values
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Invalid values test app")
	})
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module invalid-values-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o app .\nrun_command=./app\nport=-1\nrestart_delay=-5\n",
		}, nil

	case "zombie_process":
		// Process that becomes zombie
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Zombie process test app")
	})
	
	go func() {
		// This goroutine might create zombie behavior
		for i := 0; i < 10; i++ {
			go func() {
				runtime.Goexit() // Force goroutine exit
			}()
		}
	}()
	
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module zombie-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o zombie-app .\nrun_command=./zombie-app\n",
		}, nil

	case "ignore_sigterm":
		// Process that ignores SIGTERM
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Ignore SIGTERM signal
	signal.Ignore(syscall.SIGTERM)
	
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "SIGTERM ignored test app")
	})
	
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module ignore-sigterm-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o ignore-sigterm-app .\nrun_command=./ignore-sigterm-app\n",
		}, nil

	case "resource_hog":
		// Process that consumes excessive resources
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
	"runtime"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Resource hog test app")
	})
	
	// Consume CPU in background
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for {
				// CPU intensive loop
			}
		}()
	}
	
	// Consume memory
	data := make([][]byte, 0)
	go func() {
		for i := 0; i < 1000; i++ {
			chunk := make([]byte, 1024*1024) // 1MB chunks
			data = append(data, chunk)
			time.Sleep(time.Millisecond * 100)
		}
	}()
	
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module resource-hog-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o resource-hog-app .\nrun_command=./resource-hog-app\n",
		}, nil

	case "file_hog":
		// Process that opens many files
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "File hog test app")
	})
	
	// Open many file descriptors
	files := make([]*os.File, 0)
	go func() {
		for i := 0; i < 1000; i++ {
			if file, err := os.Open("/dev/null"); err == nil {
				files = append(files, file)
			}
		}
	}()
	
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module file-hog-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o file-hog-app .\nrun_command=./file-hog-app\n",
		}, nil

	case "memory_hog":
		// Process that consumes excessive memory
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Memory hog test app")
	})
	
	// Allocate large amounts of memory
	go func() {
		data := make([][]byte, 0)
		for i := 0; i < 500; i++ {
			chunk := make([]byte, 10*1024*1024) // 10MB chunks
			data = append(data, chunk)
			time.Sleep(time.Millisecond * 50)
		}
	}()
	
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module memory-hog-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o memory-hog-app .\nrun_command=./memory-hog-app\n",
		}, nil

	case "cpu_hog":
		// Process that consumes excessive CPU
		return map[string]string{
			"main.go": `package main

import (
	"fmt"
	"net/http"
	"runtime"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "CPU hog test app")
	})
	
	// Consume all CPU cores
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			for {
				// Infinite CPU-intensive loop
			}
		}()
	}
	
	http.ListenAndServe(":8080", nil)
}`,
			"go.mod":        "module cpu-hog-app\ngo 1.21\n",
			"deploy.config": "build_command=go build -o cpu-hog-app .\nrun_command=./cpu-hog-app\n",
		}, nil

	default:
		return nil, fmt.Errorf("unknown fail type: %s", failType)
	}
}
