#!/usr/bin/env python3
"""
whatsapp-claude - Connect WhatsApp to Claude AI
Built by Qalarc - https://qalarc.com

This bridge listens for incoming WhatsApp messages, passes them to Claude AI,
and sends Claude's responses back via WhatsApp.

The transport layer is the compiled `whatsapp-cli` Go binary (whatsmeow library).
This Python script handles the Claude integration and message routing.

Quick Start:
  1. Build the CLI:  cd whatsapp_cli && go build -o ../whatsapp-claude
  2. Login:          ./whatsapp-claude login
  3. Configure:      cp config.example.json config.json  (edit as needed)
  4. Run bridge:     python3 bridge.py

License: MIT
"""

import subprocess
import json
import os
import sys
import time
import signal
import re
import hashlib
from pathlib import Path
from datetime import datetime
from collections import defaultdict
from typing import Optional, Dict, List, Tuple

# ─── Paths ────────────────────────────────────────────────────────────────────

BASE_DIR = Path(__file__).parent
CLI_PATH = BASE_DIR / "whatsapp-claude"  # compiled Go binary
AUTH_DIR = BASE_DIR / "auth"  # WhatsApp session storage
CONFIG_FILE = BASE_DIR / "config.json"
LOG_FILE = BASE_DIR / "bridge.log"

# ─── Defaults ─────────────────────────────────────────────────────────────────

MAX_MESSAGE_LENGTH = 4000
CLAUDE_TIMEOUT = 300  # seconds (5 min)

# ─── In-memory state ──────────────────────────────────────────────────────────

message_history: Dict[str, Dict] = defaultdict(dict)

# ──────────────────────────────────────────────────────────────────────────────
# Config
# ──────────────────────────────────────────────────────────────────────────────


def load_config() -> dict:
    """Load config.json, creating a template if it doesn't exist."""
    if not CONFIG_FILE.exists():
        default = {
            "whatsapp": {
                "account": "",
                "cli_path": str(CLI_PATH),
                "auth_dir": str(AUTH_DIR),
            },
            "claude": {
                "path": "/usr/bin/claude",
                "timeout": CLAUDE_TIMEOUT,
                "system_prompt": (
                    "You are a helpful AI assistant reachable via WhatsApp. "
                    "Be concise - WhatsApp messages should be short and readable. "
                    "Avoid excessive formatting like headers or long bullet lists."
                ),
            },
            "users": {
                # "+15551234567": {
                #   "name": "Alice",
                #   "role": "owner"
                # }
            },
            "owner_phone": "",
            "log_level": "INFO",
        }
        CONFIG_FILE.write_text(json.dumps(default, indent=2))
        log(f"Created default config at {CONFIG_FILE} - please edit it and restart.")
    with open(CONFIG_FILE) as f:
        return json.load(f)


# ──────────────────────────────────────────────────────────────────────────────
# Logging
# ──────────────────────────────────────────────────────────────────────────────


def log(message: str, level: str = "INFO") -> None:
    ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    line = f"[{ts}] [{level}] {message}"
    print(line, flush=True)
    with open(LOG_FILE, "a") as f:
        f.write(line + "\n")


# ──────────────────────────────────────────────────────────────────────────────
# WhatsApp transport
# ──────────────────────────────────────────────────────────────────────────────


def _cli_env(config: dict) -> dict:
    """Build environment for CLI subprocess calls."""
    env = os.environ.copy()
    auth_dir = config.get("whatsapp", {}).get("auth_dir", str(AUTH_DIR))
    env["WHATSAPP_DATA_DIR"] = str(auth_dir)
    return env


def send_message(
    config: dict, recipient: str, text: str, attachments: Optional[List[str]] = None
) -> bool:
    """Send a WhatsApp message (and optional file attachments)."""
    cli = config.get("whatsapp", {}).get("cli_path", str(CLI_PATH))
    if not Path(cli).exists():
        log(f"CLI binary not found at {cli}", "ERROR")
        return False

    # Strip + prefix - CLI expects plain number or JID
    recipient = recipient.lstrip("+")

    env = _cli_env(config)
    chunks = _split(text)

    for i, chunk in enumerate(chunks):
        if len(chunks) > 1:
            chunk = f"[{i + 1}/{len(chunks)}]\n{chunk}"
        result = subprocess.run(
            [cli, "send", recipient, chunk],
            capture_output=True,
            text=True,
            timeout=60,
            env=env,
        )
        if result.returncode != 0:
            log(f"Send failed: {result.stderr.strip()}", "ERROR")
            return False
        if len(chunks) > 1:
            time.sleep(0.5)

    if attachments:
        for path in attachments:
            if os.path.exists(path):
                subprocess.run(
                    [cli, "send-file", recipient, path],
                    capture_output=True,
                    timeout=120,
                    env=env,
                )
    return True


def check_status(config: dict) -> dict:
    """Return the current login/connection status dict."""
    cli = config.get("whatsapp", {}).get("cli_path", str(CLI_PATH))
    env = _cli_env(config)
    try:
        result = subprocess.run(
            [cli, "status"], capture_output=True, text=True, timeout=30, env=env
        )
        for line in result.stdout.splitlines():
            line = line.strip()
            if line.startswith("{"):
                return json.loads(line)
    except Exception as e:
        return {"error": str(e)}
    return {"error": "no JSON output"}


# ──────────────────────────────────────────────────────────────────────────────
# Claude integration
# ──────────────────────────────────────────────────────────────────────────────

# Patterns we block unconditionally for all users (prevent prompt injection abuse)
_DANGEROUS = [
    r"rm\s+-rf\s+/",
    r"sudo\s+",
    r"mkfs\.",
    r"dd\s+if=",
    r":\(\)\s*{\s*:\s*\|\s*:",
    r">\s*/dev/sd",
    r"curl.*\|\s*bash",
    r"wget.*\|\s*bash",
]


def _is_dangerous(text: str) -> bool:
    for p in _DANGEROUS:
        if re.search(p, text, re.IGNORECASE):
            return True
    return False


def run_claude(
    prompt: str,
    config: dict,
    working_dir: Optional[str] = None,
    system_prompt: Optional[str] = None,
) -> str:
    """Invoke Claude CLI and return its response text."""
    claude_cfg = config.get("claude", {})
    claude_path = claude_cfg.get("path", "/usr/bin/claude")
    timeout = claude_cfg.get("timeout", CLAUDE_TIMEOUT)
    sys_prompt = system_prompt or claude_cfg.get("system_prompt", "")
    cwd = working_dir or str(Path.home())

    if _is_dangerous(prompt):
        log(f"Blocked dangerous pattern in prompt", "WARN")
        return "Sorry, that request isn't something I can help with."

    # Build full prompt
    full_prompt = prompt
    if sys_prompt:
        full_prompt = f"{sys_prompt}\n\n---\n\n{prompt}"

    try:
        result = subprocess.run(
            [claude_path, "--print", full_prompt],
            capture_output=True,
            text=True,
            timeout=timeout,
            cwd=cwd,
        )
        response = result.stdout.strip()
        if not response and result.stderr:
            response = f"[Claude error: {result.stderr.strip()[:200]}]"
        return response or "[No response]"
    except subprocess.TimeoutExpired:
        return f"[Timed out after {timeout // 60} min]"
    except FileNotFoundError:
        return (
            "[Claude CLI not found. Install Claude Code: "
            "https://www.anthropic.com/claude-code]"
        )
    except Exception as e:
        return f"[Error: {e}]"


# ──────────────────────────────────────────────────────────────────────────────
# User management
# ──────────────────────────────────────────────────────────────────────────────


def _normalize_phone(phone: str) -> str:
    """Strip all non-digit characters."""
    return re.sub(r"\D", "", phone)


def get_user(config: dict, sender: str) -> dict:
    """Return user config dict for a sender, or empty dict if unknown."""
    sender_norm = _normalize_phone(sender)
    for identifier, cfg in config.get("users", {}).items():
        if _normalize_phone(identifier) == sender_norm:
            return {**cfg, "authorized": True, "phone": identifier}
    return {"authorized": False}


def hash_phone(phone: str) -> str:
    """One-way hash of phone number - used as a privacy-safe identifier."""
    normalized = re.sub(r"\D", "", phone)
    return hashlib.sha256(f"whatsapp-claude:{normalized}".encode()).hexdigest()[:12]


# ──────────────────────────────────────────────────────────────────────────────
# Message helpers
# ──────────────────────────────────────────────────────────────────────────────


def _split(text: str, max_len: int = MAX_MESSAGE_LENGTH) -> List[str]:
    """Split long text into chunks that fit WhatsApp's limits."""
    if len(text) <= max_len:
        return [text]
    chunks, chunk = [], ""
    for line in text.splitlines():
        if len(chunk) + len(line) + 1 > max_len and chunk:
            chunks.append(chunk.rstrip())
            chunk = ""
        chunk += line + "\n"
    if chunk.strip():
        chunks.append(chunk.strip())
    return chunks or [text[:max_len]]


def _store_exchange(sender: str, ts: int, text: str, response: str) -> None:
    """Keep the last 20 exchanges per user for context."""
    history = message_history[sender]
    history[ts] = {"text": text, "response": response}
    if len(history) > 20:
        del history[min(history)]


def _get_context(sender: str, quote: Optional[dict]) -> str:
    """Build conversation context from reply chain."""
    if quote:
        quoted = quote.get("text", "")
        if quoted:
            return f'[Replying to: "{quoted}"]\n\n'
    return ""


# ──────────────────────────────────────────────────────────────────────────────
# Message processing
# ──────────────────────────────────────────────────────────────────────────────


def process_authorized_message(
    config: dict, sender: str, message: str, user: dict, quote: Optional[dict] = None
) -> Tuple[str, List[str]]:
    """Handle a message from an authorized user - full Claude access."""
    name = user.get("name", "User")
    role = user.get("role", "user")
    context = _get_context(sender, quote)

    log(f"Processing [{role}] {name}: {message[:80]}")

    response = run_claude(message, config, system_prompt=context or None)

    # Auto-send files tagged as [SEND_FILE: /path/to/file]
    files = re.findall(r"\[SEND_FILE:\s*([^\]]+)\]", response)
    files = [
        os.path.expanduser(f.strip())
        for f in files
        if os.path.exists(os.path.expanduser(f.strip()))
    ]
    response = re.sub(r"\[SEND_FILE:\s*[^\]]+\]\s*", "", response).strip()

    _store_exchange(sender, int(time.time() * 1000), message, response)
    return response, files


def process_public_message(
    config: dict, sender: str, message: str, sender_name: Optional[str] = None
) -> str:
    """
    Handle a message from an unknown/public user.

    By default this returns a simple "not available" response.
    You can customise this function to add your own onboarding flow,
    FAQ bot, or anything else you want public users to experience.

    ---
    Qalarc uses this for onboarding DJs to the DOOF.ING community platform.
    See https://qalarc.com for inspiration on what you can build here.
    ---
    """
    msg_lower = (message or "").lower().strip()
    log(f"Public user {hash_phone(sender)} ({sender_name}): {message[:60]}")

    # Customise or replace this block with your own logic
    system_prompt = (
        "You are a helpful AI assistant available via WhatsApp. "
        "Be friendly, concise, and helpful. "
        "If users ask who made you or what this is, say: "
        "\"I'm an AI assistant built with Claude, connected to WhatsApp "
        'using an open-source bridge by Qalarc (qalarc.com)."'
    )
    return run_claude(message, config, system_prompt=system_prompt)


# ──────────────────────────────────────────────────────────────────────────────
# Main receive loop
# ──────────────────────────────────────────────────────────────────────────────


def receive_loop(config: dict) -> None:
    cli = config.get("whatsapp", {}).get("cli_path", str(CLI_PATH))
    auth_dir = config.get("whatsapp", {}).get("auth_dir", str(AUTH_DIR))
    my_account = _normalize_phone(config.get("whatsapp", {}).get("account", ""))

    # Verify login
    status = check_status(config)
    if not status.get("logged_in"):
        log("Not logged in! Run: ./whatsapp-claude login", "ERROR")
        sys.exit(1)
    log(f"Logged in as {status.get('phone')} - bridge starting...")

    env = os.environ.copy()
    env["WHATSAPP_DATA_DIR"] = str(auth_dir)

    process = subprocess.Popen(
        [cli, "receive"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env=env,
        bufsize=1,
    )

    log("Bridge active - listening for messages.")

    stdout = process.stdout
    assert stdout is not None  # we passed stdout=PIPE

    try:
        for raw_line in stdout:
            line = raw_line.strip()
            if not line or not line.startswith("{"):
                continue
            try:
                data = json.loads(line)
            except json.JSONDecodeError:
                continue

            msg_type = data.get("type")

            if msg_type == "connected":
                log(f"Connected as {data.get('phone')}")
                continue
            if msg_type == "disconnected":
                log("Disconnected from WhatsApp", "WARN")
                continue
            if msg_type != "message":
                continue

            sender = data.get("sender", "")
            message = data.get("message", "")
            sender_name = data.get("sender_name")
            is_group = data.get("is_group", False)
            quote = data.get("quote")

            if not sender:
                continue
            if is_group:
                # Group messages: implement group handling here if desired
                continue
            # Skip messages sent from our own account
            if _normalize_phone(sender) == my_account:
                continue
            if not message:
                continue

            log(f"Message from {sender_name or hash_phone(sender)}: {message[:60]}")

            user = get_user(config, sender)

            if user.get("authorized"):
                # Send typing indicator / acknowledgment
                send_message(config, sender, "...")
                response, files = process_authorized_message(
                    config, sender, message, user, quote=quote
                )
                send_message(config, sender, response, attachments=files or None)
            else:
                response = process_public_message(config, sender, message, sender_name)
                send_message(config, sender, response)

    except KeyboardInterrupt:
        log("Shutting down bridge...")
    finally:
        process.terminate()
        process.wait()


# ──────────────────────────────────────────────────────────────────────────────
# Entry point
# ──────────────────────────────────────────────────────────────────────────────


def main() -> None:
    AUTH_DIR.mkdir(parents=True, exist_ok=True)

    def _shutdown(signum, frame):
        log("Signal received - shutting down.")
        sys.exit(0)

    signal.signal(signal.SIGINT, _shutdown)
    signal.signal(signal.SIGTERM, _shutdown)

    config = load_config()
    receive_loop(config)


if __name__ == "__main__":
    main()
