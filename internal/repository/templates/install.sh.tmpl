#!/bin/bash
set -e

# Auto-generated install script for Sleuth Skills artifact repository
# This script ensures the skills CLI is installed and configured.
# Safe to run multiple times (idempotent).

SKILLS_CONFIG="$HOME/.config/sleuth/skills/config.json"

# Check if skills CLI is already installed
if command -v skills &> /dev/null; then
    echo "✓ skills CLI is already installed ($(skills --version))"
else
    echo "Installing Sleuth Skills CLI..."
    echo

    # Install skills CLI from GitHub
    curl -fsSL https://raw.githubusercontent.com/sleuth-io/skills/main/install.sh | bash

    # Verify installation
    if ! command -v skills &> /dev/null; then
        echo "Error: skills CLI installation failed"
        echo "Please ensure ~/.local/bin is in your PATH and try again"
        exit 1
    fi

    echo "✓ skills CLI installed successfully"
fi

# Check if already configured
if [ -f "$SKILLS_CONFIG" ]; then
    echo "✓ skills CLI is already configured"
    exit 0
fi

echo
echo "Configuring skills CLI for this repository..."
echo

# Auto-detect repository URL from git remote
REPO_URL=$(git remote get-url origin 2>/dev/null || echo "")

if [ -z "$REPO_URL" ]; then
    echo "Error: Could not detect repository URL"
    echo "Please run manually: skills init --type git --repo-url <REPO_URL>"
    exit 1
fi

echo "Detected repository URL: $REPO_URL"
echo

# Configure skills CLI
skills init --type git --repo-url "$REPO_URL"

echo
echo "✓ Configuration complete!"
echo
echo "You can now use 'skills install' to install artifacts from this repository."
