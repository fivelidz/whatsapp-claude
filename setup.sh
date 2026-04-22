#!/usr/bin/env bash
# whatsapp-claude setup script
# Builds the Go binary and prepares the environment.
# Built by Qalarc - https://qalarc.com

set -e

echo "=== whatsapp-claude setup ==="
echo "https://qalarc.com"
echo ""

# Check Go
if ! command -v go &>/dev/null; then
  echo "ERROR: Go is not installed."
  echo "Install from: https://go.dev/dl/"
  exit 1
fi

# Check Python
if ! command -v python3 &>/dev/null; then
  echo "ERROR: Python 3 is not installed."
  exit 1
fi

# Check Claude CLI
if ! command -v claude &>/dev/null; then
  echo "WARNING: Claude CLI not found at /usr/bin/claude"
  echo "Install Claude Code: https://www.anthropic.com/claude-code"
  echo "(You can still build the binary, but the bridge won't work without Claude.)"
  echo ""
fi

# Build the Go binary
echo "Building whatsapp-claude binary..."
cd whatsapp_cli
go mod tidy
go build -o ../whatsapp-claude .
cd ..

echo ""
echo "Build complete: ./whatsapp-claude"
echo ""

# Create auth directory
mkdir -p auth

# Create config if missing
if [ ! -f config.json ]; then
  cp config.example.json config.json
  echo "Created config.json - edit it with your phone number and settings."
else
  echo "config.json already exists."
fi

echo ""
echo "=== Next Steps ==="
echo ""
echo "1. Edit config.json with your WhatsApp account number and settings."
echo ""
echo "2. Login to WhatsApp:"
echo "   ./whatsapp-claude login"
echo ""
echo "3. Scan the QR code with your phone:"
echo "   WhatsApp -> Settings -> Linked Devices -> Link a Device"
echo ""
echo "4. Start the bridge:"
echo "   python3 bridge.py"
echo ""
echo "=== Documentation ==="
echo "README.md       - full setup guide"
echo "https://qalarc.com - more about what you can build with this"
