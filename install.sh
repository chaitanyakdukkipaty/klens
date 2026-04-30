#!/bin/bash

# klens Installation Script
# Usage: curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/install.sh | bash
# Debug: DEBUG=true curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/install.sh | bash

set -e

# Configuration
REPO="chaitanyakdukkipaty/klens"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="klens"
DEBUG=${DEBUG:-false}

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1" >&2
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_debug() {
    if [ "$DEBUG" = "true" ]; then
        echo -e "${YELLOW}[DEBUG]${NC} $1" >&2
    fi
}

# Detect OS and architecture
detect_platform() {
    local os
    local arch

    case "$(uname -s)" in
        Darwin*)
            os="darwin"
            ;;
        Linux*)
            os="linux"
            ;;
        *)
            log_error "Unsupported operating system: $(uname -s)"
            exit 1
            ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        arm64|aarch64)
            arch="arm64"
            ;;
        *)
            log_error "Unsupported architecture: $(uname -m)"
            exit 1
            ;;
    esac

    echo "${os}-${arch}"
}

# Get latest release version
get_latest_version() {
    local version
    version=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$version" ]; then
        log_error "Failed to get latest version from GitHub API"
        log_error "This could be due to rate limiting or network issues"
        log_error "You can also check manually at: https://github.com/${REPO}/releases/latest"
        exit 1
    fi

    log_info "GitHub API returned version: $version"
    echo "$version"
}

# Download and install binary
install_binary() {
    local version="$1"
    local platform="$2"
    local tmp_dir

    tmp_dir=$(mktemp -d)

    log_info "Downloading klens $version for $platform..."

    local download_url="https://github.com/${REPO}/releases/download/${version}/klens-${version}-${platform}.tar.gz"

    log_info "Download URL: $download_url"
    log_debug "Using temp directory: $tmp_dir"

    # Check if the file exists first
    local http_status
    http_status=$(curl -s -I "$download_url" | head -n 1 | cut -d' ' -f2)

    if [ "$http_status" != "200" ] && [ "$http_status" != "302" ]; then
        log_error "Release file not found at: $download_url"
        log_error "HTTP Status: $http_status"
        log_error "Please check: https://github.com/${REPO}/releases"
        exit 1
    fi

    local archive_file="$tmp_dir/klens.tar.gz"
    log_info "Downloading to: $archive_file"

    if ! curl -sL "$download_url" -o "$archive_file"; then
        log_error "Failed to download klens"
        exit 1
    fi

    # Verify it's a valid gzip archive
    if command -v file >/dev/null 2>&1; then
        local file_type
        file_type=$(file "$archive_file" 2>/dev/null || echo "unknown")
        log_info "Downloaded file type: $file_type"

        if [[ "$file_type" != *"gzip compressed"* ]]; then
            log_error "Downloaded file is not a valid gzip archive"
            log_error "File content (first 200 chars):"
            head -c 200 "$archive_file" | cat -v
            exit 1
        fi
    else
        local magic_bytes
        magic_bytes=$(head -c 2 "$archive_file" | od -x | head -n1 | awk '{print $2}')
        if [ "$magic_bytes" != "8b1f" ]; then
            log_error "Downloaded file does not appear to be a gzip archive"
            log_error "File content (first 200 chars):"
            head -c 200 "$archive_file" | cat -v
            exit 1
        fi
    fi

    # Extract the archive
    if ! tar -xzf "$archive_file" -C "$tmp_dir"; then
        log_error "Failed to extract klens archive"
        exit 1
    fi

    # Find the binary (try versioned name first, then plain name)
    local binary_path
    local possible_names=(
        "klens-${version}-${platform}"
        "klens"
        "${BINARY_NAME}"
    )

    log_info "Looking for binary in extracted files..."
    log_debug "Archive contents:"
    ls -la "$tmp_dir" >&2

    for name in "${possible_names[@]}"; do
        if [ -f "$tmp_dir/$name" ]; then
            binary_path="$tmp_dir/$name"
            log_info "Found binary: $name"
            break
        fi
    done

    if [ -z "$binary_path" ]; then
        log_error "Binary not found in downloaded archive"
        log_error "Tried: ${possible_names[*]}"
        log_error "Available files:"
        find "$tmp_dir" -type f
        exit 1
    fi

    log_info "Installing klens to $INSTALL_DIR/$BINARY_NAME..."

    if ! cp "$binary_path" "$INSTALL_DIR/$BINARY_NAME" 2>/dev/null; then
        log_warn "Permission denied, trying with sudo..."
        sudo cp "$binary_path" "$INSTALL_DIR/$BINARY_NAME"
    fi

    if ! chmod +x "$INSTALL_DIR/$BINARY_NAME" 2>/dev/null; then
        sudo chmod +x "$INSTALL_DIR/$BINARY_NAME"
    fi

    # Remove macOS quarantine flag so Gatekeeper doesn't block the binary
    if [ "$(uname -s)" = "Darwin" ]; then
        xattr -dr com.apple.quarantine "$INSTALL_DIR/$BINARY_NAME" 2>/dev/null || true
    fi

    rm -rf "$tmp_dir"

    log_info "klens installed successfully!"
}

main() {
    log_info "Installing klens..."

    if ! command -v curl >/dev/null 2>&1; then
        log_error "curl is required but not installed."
        exit 1
    fi

    if ! command -v tar >/dev/null 2>&1; then
        log_error "tar is required but not installed."
        exit 1
    fi

    local platform
    platform=$(detect_platform)
    log_info "Detected platform: $platform"

    local version
    version=$(get_latest_version)
    log_info "Latest version: $version"

    install_binary "$version" "$platform"

    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        log_info "Verification successful! klens is now available in your PATH."
    else
        log_warn "klens was installed but may not be in your PATH."
        log_warn "Try: export PATH=\"$INSTALL_DIR:\$PATH\""
    fi

    echo ""
    log_info "klens installation complete!"
    log_info "Get started: klens --help"
}

main "$@"
