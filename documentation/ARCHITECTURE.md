# System Architecture

The versioned Docker build system is designed as a modular Go application with the following components:

## Directory Structure
- `cmd/apis/main.go`: Entry point for both CLI and REST API.
- `internal/versioning/`: Handles version history in `versions.json`.
- `internal/docker/`: Wraps Docker CLI for container lifecycle management.
- `internal/cache/`: Existing hashing and local cache logic.
- `internal/config/`: Configuration parsing (`builder.yaml`).
- `.builder-cache/<feature>/`: Metadata storage.
  - `hash`: Current build hash.
  - `versions.json`: Chronological record of all builds.

## Metadata Tracking
Each build produces a `VersionRecord` stored in `versions.json`:
- `version`: Sequential integer.
- `full_tag`: Remote image reference (e.g., Harbor).
- `short_hash`: 8-character build hash.
- `input_hash`: Full SHA256 build hash.
- `dependencies`: List of feature dependencies.
- `build_command`: Command used for the build.

## Isolation and Promotion
- **Container Isolation**: Deterministic naming (`builder_<feature>_<role>_<hash>`) prevents conflicts between different versions or features.
- **Promotion**: When a build succeeds, the image is run as `v<N>`, then the old `current` container is stopped, and the new image is re-run with the `current` role.
