# Project Context

## Reference Repos
Two Avalanche repos are cloned in the home directory:
- `~/avalanchego` (v1.14.0) - main Avalanche node
- `~/subnet-evm` (v0.8.0) - subnet EVM implementation

Use `grep` to search these repos when you need to understand how something works.

## Build Rules
All build outputs must go to `/tmp/` - never build in `/app` or any directory that could end up in git history.
