# whatsapp-claude

> **Send a WhatsApp message → get a reply from Claude AI.**  
> Self-hosted. No cloud middleman. Runs on a Raspberry Pi or any Linux box.

Built by [Qalarc](https://qalarc.com) — open sourced for anyone to use.

---

## How It Works

```
You send a WhatsApp message
        ↓
whatsapp-claude (Go binary using whatsmeow)
  connects as a Linked Device, receives the message
        ↓
bridge.py (Python)
  passes the message to Claude CLI:  claude --print "your message"
        ↓
Claude responds
        ↓
bridge.py sends the reply back via WhatsApp
        ↓
You receive Claude's response in WhatsApp
```

That's it. Two moving parts — a Go binary that handles the WhatsApp protocol, and a Python script that calls Claude.

---

## What You Can Build With This

- **Personal AI assistant** on your own WhatsApp number — text it from anywhere
- **Public chatbot** — anyone who messages your number gets an AI reply
- **Custom onboarding flows** — state machine conversations for new users
- **Business automations** — intake forms, FAQs, booking flows over WhatsApp

We use this at [DOOF.ING](https://doof.ing) to let DJs text a WhatsApp number and automatically get an AI-generated profile page built for them.

---

## Prerequisites

| Tool | Notes | Install |
|------|-------|---------|
| **Go 1.21+** | To compile the WhatsApp binary | [go.dev/dl](https://go.dev/dl/) |
| **Python 3.9+** | For the bridge | System package manager |
| **Claude Code CLI** | The AI engine | [anthropic.com/claude-code](https://www.anthropic.com/claude-code) |
| **A WhatsApp number** | Used as a Linked Device (like WhatsApp Web) | Any WhatsApp account |

> ⚠️ **Important:** This uses the unofficial WhatsApp Web protocol via [whatsmeow](https://github.com/tulir/whatsmeow).
> WhatsApp may ban numbers that automate messages. **Use a dedicated secondary number**, not your personal one.

---

## Quick Start

```bash
# 1. Clone
git clone https://github.com/fivelidz/whatsapp-claude
cd whatsapp-claude

# 2. Build the Go binary and create config
chmod +x setup.sh && ./setup.sh

# 3. Edit config.json with your phone number
nano config.json

# 4. Login — scan QR code with WhatsApp on your phone
#    WhatsApp → Settings → Linked Devices → Link a Device
./whatsapp-claude login

# 5. Run the bridge
python3 bridge.py
```

After step 5, anyone who messages your WhatsApp number gets a Claude reply.

---

## Configuration (`config.json`)

```json
{
  "whatsapp": {
    "account": "+15551234567",
    "cli_path": "./whatsapp-claude",
    "auth_dir": "./auth"
  },
  "claude": {
    "path": "/usr/bin/claude",
    "timeout": 300,
    "system_prompt": "You are a helpful assistant reachable via WhatsApp. Be concise."
  },
  "users": {
    "+15551234567": {
      "name": "Your Name",
      "role": "owner"
    }
  },
  "owner_phone": "+15551234567"
}
```

**Authorized users** (in the `users` list) get full Claude access — it behaves like a personal AI assistant.

**Everyone else** goes through `process_public_message()` in `bridge.py` — customise this with your own welcome flow, FAQ, onboarding, etc.

---

## Customising What Public Users See

Edit `process_public_message()` in `bridge.py`:

```python
def process_public_message(config, sender, message, sender_name=None):
    # Replace this with whatever you want:
    system_prompt = "You are a helpful assistant for Acme Corp. Answer questions about our products."
    return run_claude(message, config, system_prompt=system_prompt)
```

You can also build multi-step flows — track user state in a dict, ask questions, collect info. See [DOOF.ING](https://doof.ing) for an example of a full onboarding flow built on top of this.

---

## CLI Commands

The compiled `whatsapp-claude` binary works standalone too:

```bash
./whatsapp-claude login                        # Login via QR code
./whatsapp-claude login                        # Or set WHATSAPP_PHONE=+15551234567 for pairing code
./whatsapp-claude receive                      # Stream incoming messages as JSON
./whatsapp-claude send 15551234567 "Hi there"  # Send a message
./whatsapp-claude send-file 15551234567 ./file.pdf "Here's the doc"
./whatsapp-claude status                       # Check connection
./whatsapp-claude logout                       # Log out and clear session
```

Environment variables:
| Variable | Default | Description |
|----------|---------|-------------|
| `WHATSAPP_DATA_DIR` | `./auth` | Where session data is stored |
| `WHATSAPP_PHONE` | — | Phone number for pairing code login |
| `WHATSAPP_LOG_LEVEL` | `WARN` | `DEBUG` / `INFO` / `WARN` |

---

## Running as a Background Service (systemd)

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
Environment=PYTHONUNBUFFERED=1

[Install]
WantedBy=default.target
```

```bash
systemctl --user enable --now whatsapp-claude
journalctl --user -u whatsapp-claude -f
```

---

## Project Structure

```
whatsapp-claude/
├── whatsapp_cli/
│   ├── main.go        ← Go binary: WhatsApp Web protocol (whatsmeow)
│   └── go.mod
├── bridge.py          ← Python: Claude integration + message routing
├── config.example.json
├── setup.sh           ← Builds Go binary, creates config
└── auth/              ← WhatsApp session (gitignored, keep private)
```

---

## Comparing with signal-claude

| | whatsapp-claude | [signal-claude](https://github.com/fivelidz/signal-claude) |
|---|---|---|
| Reach | 2 billion+ users | Privacy-focused users |
| Protocol | Unofficial (whatsmeow) | Official (signal-cli) |
| Ban risk | Medium — use a secondary number | None |
| Transport | Spawn process per send | Persistent socket connection |
| Typing indicators | No | Yes |
| Setup | Build Go binary + QR login | Install Java + signal-cli |

---

## Privacy

- Phone numbers are **never logged in plaintext** — SHA-256 hashed in all log output
- WhatsApp session stored locally in `./auth/whatsapp.db` — keep this file private, it's your login
- No data sent anywhere except to WhatsApp and Anthropic (Claude)

---

## Limitations

- **Unofficial protocol** — whatsmeow reverse-engineers WhatsApp Web. WhatsApp can ban numbers. Use a secondary number.
- **Single session** — one number, one linked device at a time
- **Claude CLI required** — this calls `claude --print` as a subprocess. Install [Claude Code](https://www.anthropic.com/claude-code).
- **No media download** — incoming image/audio/video metadata is captured but files aren't downloaded by default

---

## Built by Qalarc

[Qalarc](https://qalarc.com) is an AI consulting studio. We build real AI integrations that solve real problems.

We run this bridge in production at [DOOF.ING](https://doof.ing) — a music community platform where DJs text a WhatsApp number to get an AI-generated artist profile page.

**Need help building something like this?** → [qalarc.com](https://qalarc.com)

---

## Credits

- [whatsmeow](https://github.com/tulir/whatsmeow) by Tulir Asokan — Go library for WhatsApp Web
- [Claude Code](https://www.anthropic.com/claude-code) by Anthropic

## License

MIT — free to use, fork, and build on. Attribution appreciated.
