# CLI Usage Guide

The `builder` tool (implemented in `cmd/apis/main.go`) provides several subcommands for managing the build and deployment lifecycle of features.

## General Usage
```bash
go run cmd/apis/main.go <command> [args]
```

## Subcommands

### 1. `server`
Starts the Gin-based REST API server.
```bash
go run cmd/apis/main.go server
```
- Listens on port `8080`.
- Endpoint: `POST /build` with JSON body `{"feature": "feature_name"}`.

### 2. `build`
Builds a feature, pushes it to Harbor, and promotes it to current.
```bash
go run cmd/apis/main.go build <feature_name>
# OR
go run cmd/apis/main.go build --feature <feature_name>
```

### 3. `versions`
Lists the version history for a specific feature.
```bash
go run cmd/apis/main.go versions <feature_name> [--last N] [--json]
```
- `--last N`: Show only the last N versions.
- `--json`: Output the history in raw JSON format.

### 4. `run`
Runs a specific version or tag of a feature.
```bash
go run cmd/apis/main.go run <feature_name> [--version N] [--tag <hash>]
```
- If no version or tag is provided, it defaults to the latest version.
- Naming convention: `builder_<feature>_v<N>_<hash>`.

### 5. `gc`
Garbage collects stale (non-current) containers for a feature.
```bash
go run cmd/apis/main.go gc <feature_name> [--keep N]
```
- `--keep N`: Number of most recent non-current containers to preserve (default: 5).
