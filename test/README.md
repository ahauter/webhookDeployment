## Quick Start

```bash
# Run all tests
go test ./test/...

# Run tests with verbose output
go test -v ./test/...

# Run specific test suites
go test -v ./test/ -run TestTargetAppDeploymentFlow
go test -v ./test/ -run TestWebhookSecurity
go test -v ./test/ -run TestWebhookLogic

# Run tests with coverage
go test -cover ./test/...
```

