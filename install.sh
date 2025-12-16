#!/bin/bash
set -e

# Install script for sx CLI
# Downloads the latest release binary from GitHub

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Map architecture names to match GoReleaser output
case "$ARCH" in
    x86_64)
        ARCH="x86_64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Map OS names to match GoReleaser output
case "$OS" in
    linux)
        OS="Linux"
        EXT="tar.gz"
        ;;
    darwin)
        OS="Darwin"
        EXT="tar.gz"
        ;;
    mingw*|msys*|cygwin*)
        OS="Windows"
        EXT="zip"
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

# Get latest release version
echo "Fetching latest release..."
VERSION=$(curl -s https://api.github.com/repos/sleuth-io/skills/releases/latest | grep '"tag_name"' | cut -d'"' -f4)

if [ -z "$VERSION" ]; then
    echo "Error: Could not fetch latest version"
    exit 1
fi

echo "Installing sx ${VERSION} for ${OS}_${ARCH}..."

# Build download URL
BINARY_NAME="sx_${OS}_${ARCH}.${EXT}"
URL="https://github.com/sleuth-io/skills/releases/download/${VERSION}/${BINARY_NAME}"

# Determine install location
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$INSTALL_DIR"

# Download and extract
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

echo "Downloading from ${URL}..."
if ! curl -fsSL "$URL" -o "$BINARY_NAME"; then
    echo "Error: Failed to download binary"
    rm -rf "$TEMP_DIR"
    exit 1
fi

# Extract based on file type
if [ "$EXT" = "tar.gz" ]; then
    tar -xzf "$BINARY_NAME"
elif [ "$EXT" = "zip" ]; then
    unzip -q "$BINARY_NAME"
fi

# Install binary
chmod +x sx
mv sx "$INSTALL_DIR/"

# Cleanup
cd - > /dev/null
rm -rf "$TEMP_DIR"

echo "✓ sx installed to $INSTALL_DIR/sx"

# Check if install dir is in PATH
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo "⚠ Warning: $INSTALL_DIR is not in your PATH"
    echo "Add this to your ~/.bashrc or ~/.zshrc:"
    echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
fi

# Verify installation
if command -v sx &> /dev/null; then
    echo ""
    sx --version
else
    echo ""
    echo "Run 'source ~/.bashrc' (or restart your shell) and then try: sx --version"
fi
