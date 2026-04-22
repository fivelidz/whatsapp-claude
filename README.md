# whatsapp-claude

**Connect WhatsApp to Claude AI.** Send a WhatsApp message, get an AI reply.

Built by [Qalarc](https://qalarc.com) — the AI consulting arm of DOOF.ING.

---

## What This Is

A minimal, self-hosted bridge that:

1. Connects your WhatsApp number as a Linked Device (WhatsApp Web protocol)
2. Listens for incoming messages in real time
3. Passes them to [Claude Code](https://www.anthropic.com/claude-code) via the CLI
4. Sends Claude's response back to the sender

No cloud service. No third-party API keys (beyond WhatsApp and Claude). Runs on any Linux machine.

---

## Architecture

```
WhatsApp servers (WebSocket / E2E encrypted)
        │
        ▼
whatsapp-claude  (Go binary, built on whatsmeow)
  ─ login / receive / send subcommands
  ─ emits newline-delimited JSON to stdout
        │  stdout
        ▼
bridge.py  (Python)
  ─ spawns whatsapp-claude receive as subprocess
  ─ parses incoming message JSON
  ─ calls:  claude --print "<message>"
  ─ sends response back via whatsapp-claude send
```

The Go binary (`whatsapp_cli/main.go`) handles the WhatsApp protocol.  
The Python bridge (`bridge.py`) handles Claude and your custom logic.

---

## Prerequisites

| Tool | Install |
|------|---------|
| Go 1.21+ | [go.dev/dl](https://go.dev/dl/) |
| Python 3.9+ | System package manager |
| Claude Code CLI | [anthropic.com/claude-code](https://www.anthropic.com/claude-code) |
| A WhatsApp account | Linked Devices must be available on your plan |

> **Note:** This uses the unofficial WhatsApp Web protocol via [whatsmeow](https://github.com/tulir/whatsmeow).  
> WhatsApp may ban numbers that use unofficial clients. Use a secondary number for testing.

---

## Quick Start

```bash
# 1. Clone
git clone https://github.com/qalarc/whatsapp-claude
cd whatsapp-claude

# 2. Build and configure
chmod +x setup.sh
./setup.sh

# 3. Edit your config
nano config.json

# 4. Login (scan QR code)
./whatsapp-claude login

# 5. Start the bridge
python3 bridge.py
```

---

## Configuration

Copy `config.example.json` to `config.json` and edit:

```json
{
  "whatsapp": {
    "account": "+15551234567",       // your WhatsApp number
    "cli_path": "./whatsapp-claude", // path to compiled binary
    "auth_dir": "./auth"             // session storage
  },
  "claude": {
    "path": "/usr/bin/claude",
    "timeout": 300,
    "system_prompt": "You are a helpful assistant reachable via WhatsApp."
  },
  "users": {
    "+15551234567": {
      "name": "Your Name",
      "role": "owner"               // gets full Claude access
    }
  },
  "owner_phone": "+15551234567"
}
```

### User Roles

| Role | What they get |
|------|--------------|
| `owner` | Full Claude access, all capabilities |
| (unlisted) | Goes through `process_public_message()` in `bridge.py` — customise this |

---

## Customising the Public Flow

Edit `process_public_message()` in `bridge.py` to control what happens when someone who isn't in your `users` list messages you:

```python
def process_public_message(config, sender, message, sender_name=None):
    # Add your own logic here:
    # - FAQ bot
    # - Onboarding flow
    # - Intake form
    # - Or just a "sorry, not available" message
    return run_claude(message, config, system_prompt="Your custom system prompt")
```

---

## Manual CLI Usage

The `whatsapp-claude` binary can also be used standalone:

```bash
# Login
./whatsapp-claude login

# Listen (streams JSON to stdout)
./whatsapp-claude receive

# Send
./whatsapp-claude send 15551234567 "Hello from the CLI"

# Send a file
./whatsapp-claude send-file 15551234567 ./report.pdf "Here's your report"

# Check status
./whatsapp-claude status

# Logout
./whatsapp-claude logout
```

Environment variables:
- `WHATSAPP_DATA_DIR` - where to store session data (default: `./auth`)
- `WHATSAPP_PHONE` - phone number for pairing code login (alternative to QR)
- `WHATSAPP_LOG_LEVEL` - log verbosity: `DEBUG`, `INFO`, `WARN` (default: `WARN`)

---

## Running as a Service (systemd)

```ini
# ~/.config/systemd/user/whatsapp-claude.service
[Unit]
Description=WhatsApp Claude Bridge
After=network.target

[Service]
WorkingDirectory=/path/to/whatsapp-claude
ExecStart=/usr/bin/python3 /path/to/whatsapp-claude/bridge.py
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
```

```bash
systemctl --user enable whatsapp-claude
systemctl --user start whatsapp-claude
journalctl --user -u whatsapp-claude -f
```

---

## Privacy

- Sender phone numbers are never logged in plain text. They are SHA-256 hashed before appearing in any log output.
- WhatsApp session credentials are stored locally in `./auth/whatsapp.db` — keep this file private.
- No data is sent to any third party beyond WhatsApp and Anthropic (Claude).

---

## Limitations

- **Unofficial protocol** - WhatsApp can ban numbers using non-official clients
- **Single device** - One WhatsApp account = one linked device session
- **No media download** - Attachment metadata is captured but content isn't downloaded by default
- **Claude must be installed** - This calls `claude --print` as a subprocess; you need [Claude Code](https://www.anthropic.com/claude-code)

---

## Built by Qalarc

[Qalarc](https://qalarc.com) builds AI consulting tools and community platforms.  
We use this bridge ourselves to run [DOOF.ING](https://doof.ing) — a community platform for DJs and artists — where people can text a WhatsApp number to create their own DJ profile page using AI.

**Want help building something similar?** [qalarc.com](https://qalarc.com)

---

## Credits

- [whatsmeow](https://github.com/tulir/whatsmeow) by Tulir Asokan — the Go WhatsApp Web library
- [Claude Code](https://www.anthropic.com/claude-code) by Anthropic

## License

MIT — use freely, attribution appreciated.
