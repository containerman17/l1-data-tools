#!/bin/bash
# Build image and run Claude Code directly
set -ex

IMAGE_NAME="claude-dev:latest"
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

# Get current user info
USER_ID=$(id -u)
GROUP_ID=$(id -g)
USERNAME=$(whoami)

# Build unless --skip is passed
if [ "$1" != "--skip" ]; then
    echo "🔨 Building Docker image for user $USERNAME ($USER_ID:$GROUP_ID)..."
    docker build --pull \
        --build-arg USER_ID=$USER_ID \
        --build-arg GROUP_ID=$GROUP_ID \
        --build-arg USERNAME=$USERNAME \
        -f "$SCRIPT_DIR/claude.Dockerfile" \
        -t $IMAGE_NAME "$SCRIPT_DIR"
    echo "✅ Build complete!"
fi

# Ensure config exists with correct permissions (fix any root-owned files from previous runs)
mkdir -p "$HOME/.claude"
touch "$HOME/.claude.json"
if [ -d "$HOME/.claude" ]; then
    sudo chown -R $USER_ID:$GROUP_ID "$HOME/.claude" "$HOME/.claude.json" 2>/dev/null || true
fi

# Run Claude as current user
echo "🚀 Starting Claude Code in /app..."
docker run -it --rm \
    --network host \
    --user $USER_ID:$GROUP_ID \
    -e HOME=/home/$USERNAME \
    -v "$SCRIPT_DIR":/app \
    -v "$HOME/.claude":/home/$USERNAME/.claude \
    -v "$HOME/.claude.json":/home/$USERNAME/.claude.json \
    -w /app \
    $IMAGE_NAME \
    claude --dangerously-skip-permissions
