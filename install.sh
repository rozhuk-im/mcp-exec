#!/bin/bash
# Installation script for mcp-exec
# Runs on Linux (systemd) and on the BSDs — FreeBSD/DragonFlyBSD/OpenBSD/NetBSD
# have their own init system (rc.d), so the systemd unit is skipped there.

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/mcp-exec"
DEFAULT_DIR="/etc/default"
LOG_DIR="/var/log"
SYSTEMD_DIR="/etc/systemd/system"
SERVICE_USER="mcp-exec"
OS_TAG="$(uname -s)"   # Linux, FreeBSD, DragonFly, NetBSD, OpenBSD, Darwin

echo -e "${GREEN}MCP Exec Installation Script${NC}"
echo "  host OS: ${OS_TAG}"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${YELLOW}Warning: Not running as root. Some features may require sudo.${NC}"
    echo "Use 'sudo ./install.sh' for system-wide installation."
    echo ""
    read -p "Install to current directory instead? [Y/n] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Nn]$ ]]; then
        INSTALL_DIR="."
        CONFIG_DIR="./config"
        LOG_DIR="./logs"
        SYSTEMD_DIR=""
    fi
fi

# systemd exists only on Linux — on the BSDs (and macOS) the unit is not installed
if [ -n "$SYSTEMD_DIR" ] && [ "$OS_TAG" != "Linux" ]; then
    echo -e "${YELLOW}No systemd on ${OS_TAG}, skipping the unit file.${NC}"
    echo -e "${YELLOW}Use the native init system: rc.d on FreeBSD/DragonFlyBSD/OpenBSD/NetBSD,${NC}"
    echo -e "${YELLOW}launchd on macOS. 'daemon=true' in the config (re-exec + setsid) also works.${NC}"
    SYSTEMD_DIR=""
fi

# Create directories
echo -e "${GREEN}Creating directories...${NC}"
mkdir -p "$INSTALL_DIR"
mkdir -p "$CONFIG_DIR"
mkdir -p "$LOG_DIR"

# Build if binary doesn't exist
if [ ! -f "mcp-exec" ]; then
    echo -e "${GREEN}Building mcp-exec...${NC}"
    go build -o mcp-exec ./cmd/mcp-exec
fi

# Copy binary
echo -e "${GREEN}Installing binary to $INSTALL_DIR...${NC}"
cp mcp-exec "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/mcp-exec"

# Copy configuration
echo -e "${GREEN}Installing configuration...${NC}"
cp mcp-exec.ini "$CONFIG_DIR/mcp-exec.ini"

# Create service user (matches mcp-exec.service; useful on every platform)
if [ "$EUID" -eq 0 ] && ! id "$SERVICE_USER" >/dev/null 2>&1; then
    echo -e "${GREEN}Creating service user $SERVICE_USER...${NC}"
    case "$OS_TAG" in
        Linux)
            groupadd -r "$SERVICE_USER" 2>/dev/null || true
            useradd -r -g "$SERVICE_USER" -s /usr/sbin/nologin -d /nonexistent "$SERVICE_USER" 2>/dev/null || true
            ;;
        FreeBSD|DragonFly)
            # pw(8) is the BSD user administration tool on FreeBSD and DragonFly BSD
            pw groupadd -q -n "$SERVICE_USER" 2>/dev/null || true
            pw useradd -q -n "$SERVICE_USER" -g "$SERVICE_USER" -s /usr/sbin/nologin -d /nonexistent -c "mcp-exec service" -w no 2>/dev/null || true
            ;;
        NetBSD)
            groupadd "$SERVICE_USER" 2>/dev/null || true
            useradd -g "$SERVICE_USER" -d /nonexistent -s /usr/sbin/nologin -c "mcp-exec service" "$SERVICE_USER" 2>/dev/null || true
            ;;
        OpenBSD)
            echo -e "${YELLOW}On OpenBSD create the user manually with adduser(1)${NC}"
            ;;
    esac
    if ! id "$SERVICE_USER" >/dev/null 2>&1; then
        echo -e "${YELLOW}Could not create user $SERVICE_USER, create it manually${NC}"
    fi
fi

# Install environment file referenced by the systemd unit (EnvironmentFile=-/etc/default/mcp-exec)
if [ -n "$SYSTEMD_DIR" ]; then
    echo -e "${GREEN}Installing environment configuration...${NC}"
    mkdir -p "$DEFAULT_DIR"
    cp mcp-exec.default "$DEFAULT_DIR/mcp-exec"
fi

# Copy systemd files
if [ -n "$SYSTEMD_DIR" ]; then
    echo -e "${GREEN}Installing systemd service...${NC}"
    cp mcp-exec.service "$SYSTEMD_DIR/"
fi

echo ""
echo -e "${GREEN}Installation complete!${NC}"
echo ""
echo "Binary:     $INSTALL_DIR/mcp-exec"
echo "Config:     $CONFIG_DIR/mcp-exec.ini"
echo "Logs:       $LOG_DIR/mcp-exec.log"

if [ -n "$SYSTEMD_DIR" ]; then
    echo ""
    echo -e "${YELLOW}Systemd service installed. To enable and start:${NC}"
    echo "  sudo systemctl daemon-reload"
    echo "  sudo systemctl enable mcp-exec.service"
    echo "  sudo systemctl start mcp-exec.service"
    echo ""
    echo -e "${YELLOW}To view logs:${NC}"
    echo "  sudo journalctl -u mcp-exec -f"
else
    echo ""
    echo -e "${YELLOW}No systemd unit installed on ${OS_TAG}.${NC}"
    echo -e "${YELLOW}On FreeBSD/DragonFlyBSD/OpenBSD/NetBSD put a rc.d script${NC}"
    echo -e "${YELLOW}'mcp-exec' (see the README platform matrix) or run the binary${NC}"
    echo -e "${YELLOW}with daemon=true in the config; on macOS use launchd.${NC}"
fi

echo ""
echo -e "${GREEN}Run '$INSTALL_DIR/mcp-exec --help' for usage information.${NC}"
