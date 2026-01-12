# Project Context

## Reference Repos
Two Avalanche repos are cloned in the home directory:
- `~/avalanchego` (v1.14.0) - main Avalanche node
- `~/subnet-evm` (v0.8.0) - subnet EVM implementation

Use `grep` to search these repos when you need to understand how something works.

## Searching Reference Repos

To search `~/avalanchego` or `~/subnet-evm`, delegate to a one-shot Claude to preserve context in the main thread:
```bash
cd ~/avalanchego && claude -p "YOUR QUESTION" --model sonnet --allowedTools "Read,Glob,Grep,LS"
```

## Build Rules
All build outputs must go to `/tmp/` - never build in `/app` or any directory that could end up in git history.
