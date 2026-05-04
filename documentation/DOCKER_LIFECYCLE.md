# Docker Lifecycle Management

The system manages containers using Docker labels and a deterministic naming convention.

## Container Naming
`builder_<feature>_<role>_<short_hash>`
- `feature`: Name of the feature (underscores replaced by hyphens).
- `role`: Either `current` or `v<N>`.
- `short_hash`: First 8 characters of the build hash.

## Docker Labels
All managed containers are tagged with the following labels:
- `builder.feature`: The feature name.
- `builder.role`: The role (`current` or `v<N>`).
- `builder.hash`: The build hash.
- `builder.version`: The sequential version number.
- `builder.managed=true`: Marker for GC and tracking.

## Garbage Collection (GC)
The `gc` command uses labels to identify stale containers.
1. Filters containers by `builder.feature`.
2. Excludes those with `builder.role=current`.
3. Sorts remaining containers by creation date (descending).
4. Keeps the first `N` containers.
5. Stops and removes the rest.
