> [!CAUTION]
> **NEVER CUT CORNERS.** If tests don't pass, if data is missing or incorrect, if something seems wrong or data appears inconsistent—**STOP and ask the user for input.** Do not proceed with assumptions, workarounds, or incomplete implementations. This is critical for data integrity.

# Project Context

## Reference Repos
Two Avalanche repos are cloned in the home directory:
- `~/avalanchego` (v1.14.0) - main Avalanche node
- `~/subnet-evm` (v0.8.0) - subnet EVM implementation

Use `grep` to search these repos when you need to understand how something works or delegate to a one-shot Claude to preserve context in the main thread.

## Searching Reference Repos

To search `~/avalanchego` or `~/subnet-evm`, delegate to a one-shot Claude to preserve context in the main thread:
```bash
cd ~/avalanchego && claude -p "YOUR QUESTION" --model sonnet --allowedTools "Read,Glob,Grep,LS"
```

## Build Rules
All build outputs must go to `/tmp/` - never build in `/app` or any directory that could end up in git history.
