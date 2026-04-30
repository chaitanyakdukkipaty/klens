#!/bin/bash

# klens Uninstall Script
# Usage: curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/uninstall.sh | bash
# Keep config: curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/uninstall.sh | KEEP_DATA=true bash

set -e

INSTALL_DIR="/usr/local/bin"
BINARY_NAME="klens"
CONFIG_DIR="${HOME}/.config/klens"
KEEP_DATA=${KEEP_DATA:-false}

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1" >&2; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1" >&2; }

remove_binary() {
    local binary_path="$INSTALL_DIR/$BINARY_NAME"

    if [ -f "$binary_path" ]; then
        log_info "Removing $binary_path..."
        if ! rm -f "$binary_path" 2>/dev/null; then
            log_warn "Permission denied, trying with sudo..."
            sudo rm -f "$binary_path"
        fi
        log_info "  ✓ Binary removed"
    else
        log_warn "Binary not found at $binary_path (already removed?)"
    fi
}

remove_config() {
    if [ "$KEEP_DATA" = "true" ]; then
        log_info "KEEP_DATA=true — skipping config removal."
        return
    fi

    if [ -d "$CONFIG_DIR" ]; then
        log_info "Removing $CONFIG_DIR..."
        rm -rf "$CONFIG_DIR"
        log_info "  ✓ Config removed"
    fi
}

main() {
    echo ""
    log_info "Uninstalling klens..."
    echo ""

    remove_binary
    remove_config

    echo ""
    log_info "✅ klens has been uninstalled."

    if [ "$KEEP_DATA" = "true" ]; then
        log_info "   Config kept (KEEP_DATA=true)."
    fi

    log_info "   To reinstall: curl -sSL https://raw.githubusercontent.com/chaitanyakdukkipaty/klens/main/install.sh | bash"
    echo ""
}

main "$@"
