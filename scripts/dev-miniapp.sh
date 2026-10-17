#!/usr/bin/env bash
# One-command local Mini App test:
#   Go API + Vite dev server + Cloudflare HTTPS tunnel + Telegram menu button.
#
# Usage:
#   TELEGRAM_BOT_TOKEN='123:abc' ALLOWED_TELEGRAM_USER_IDS='123456789' ./scripts/dev-miniapp.sh
#
# The token is never printed. A per-run test database is used unless DATABASE_PATH is set.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

log_dir="$(mktemp -d "${TMPDIR:-/tmp}/miniapp-dev.XXXXXX")"
api_log="$log_dir/api.log"
vite_log="$log_dir/vite.log"
tunnel_log="$log_dir/tunnel.log"
api_pid=""
vite_pid=""
tunnel_pid=""

cleanup() {
  trap - EXIT INT TERM
  for pid in "$tunnel_pid" "$vite_pid" "$api_pid"; do
    [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  echo "stopped; logs: $log_dir"
}
trap cleanup EXIT INT TERM

fail() {
  echo "error: $*" >&2
  for log in "$api_log" "$vite_log" "$tunnel_log"; do
    [[ -s "$log" ]] && { echo "--- $(basename "$log")" >&2; tail -n 20 "$log" >&2; }
  done
  exit 1
}

# main.go gives a non-empty .env token precedence over the environment; mirror it
# so the Bot API call and the Go process always use the same bot.
token=""
if [[ -f .env ]]; then
  token="$(awk -F= '$1=="TELEGRAM_BOT_TOKEN"{sub(/^TELEGRAM_BOT_TOKEN=/,""); print; exit}' .env | tr -d '\r')"
fi
[[ -n "$token" ]] || token="${TELEGRAM_BOT_TOKEN:-}"
if [[ -z "$token" ]]; then
  echo "error: set TELEGRAM_BOT_TOKEN or add it to .env" >&2
  exit 1
fi
if [[ -z "${ALLOWED_TELEGRAM_USER_IDS:-}" ]]; then
  echo "error: export ALLOWED_TELEGRAM_USER_IDS='<your Telegram user id>'" >&2
  exit 1
fi
chat_id="$(printf '%s' "$ALLOWED_TELEGRAM_USER_IDS" | awk -F, '{print $1}' | tr -d '[:space:]')"
[[ "$chat_id" =~ ^[1-9][0-9]*$ ]] || { echo "error: ALLOWED_TELEGRAM_USER_IDS must start with a positive user ID" >&2; exit 1; }

node_major() { node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0; }
if [[ -n "${MINIAPP_NODE_BIN:-}" ]]; then
  PATH="$(dirname "$MINIAPP_NODE_BIN"):$PATH"
  export PATH
elif [[ "$(node_major)" != "24" ]]; then
  node_version="${MINIAPP_NODE_VERSION:-24.22.0}"
  case "$(uname -m)" in
    x86_64) node_arch="linux-x64" ;;
    aarch64|arm64) node_arch="linux-arm64" ;;
    *) echo "error: unsupported architecture $(uname -m)" >&2; exit 1 ;;
  esac
  node_home="$root/.cache/node-v$node_version-$node_arch"
  if [[ ! -x "$node_home/bin/node" ]]; then
    archive="node-v$node_version-$node_arch.tar.xz"
    mkdir -p "$root/.cache"
    echo "downloading Node $node_version…"
    curl -fsSL "https://nodejs.org/dist/v$node_version/$archive" -o "$root/.cache/$archive" || {
      echo "error: cannot download Node $node_version" >&2
      exit 1
    }
    expected="$(curl -fsSL "https://nodejs.org/dist/v$node_version/SHASUMS256.txt" | awk -v f="$archive" '$2==f{print $1}')"
    [[ -n "$expected" ]] || { echo "error: Node checksum unavailable" >&2; exit 1; }
    printf '%s  %s\n' "$expected" "$root/.cache/$archive" | sha256sum -c - >/dev/null || {
      echo "error: Node archive checksum mismatch" >&2
      exit 1
    }
    tar -xJf "$root/.cache/$archive" -C "$root/.cache"
    rm -f "$root/.cache/$archive"
  fi
  PATH="$node_home/bin:$PATH"
  export PATH
fi
[[ "$(node_major)" == "24" ]] || { echo "error: Node 24 is required" >&2; exit 1; }
public_url="${MINIAPP_PUBLIC_URL:-}"
if [[ -n "$public_url" ]]; then
  public_base="${public_url%/}"
  [[ "$public_base" == */app ]] && public_base="${public_base%/app}"
  [[ "$public_base" =~ ^https://[A-Za-z0-9.-]+$ ]] || {
    echo "error: MINIAPP_PUBLIC_URL must be https://<host> or https://<host>/app" >&2
    exit 1
  }
  export MINIAPP_PUBLIC_URL="$public_base"
else
  command -v cloudflared >/dev/null || { echo "error: cloudflared is required (or set MINIAPP_PUBLIC_URL)" >&2; exit 1; }
fi

for port in 8080 5173; do
  if ss -ltn 2>/dev/null | awk '{print $4}' | grep -q ":$port$"; then
    echo "error: port $port is already in use; stop the existing instance first" >&2
    exit 1
  fi
done

export TELEGRAM_BOT_TOKEN="$token"
export DATABASE_PATH="${DATABASE_PATH:-./data/miniapp-test.db}"
mkdir -p "$(dirname "$DATABASE_PATH")"

echo "installing frontend dependencies…"
(cd web && npm ci --silent) || fail "npm ci failed"

echo "building Go API…"
GOTOOLCHAIN=local go build -trimpath -o ./bin/remind . || fail "go build failed"

./bin/remind >"$api_log" 2>&1 &
api_pid=$!
ready=""
for _ in $(seq 1 80); do
  if curl -fsS http://127.0.0.1:8080/readyz >/dev/null 2>&1; then ready=1; break; fi
  kill -0 "$api_pid" 2>/dev/null || break
  sleep 0.25
done
[[ -n "$ready" ]] || fail "Go API did not become ready"
(cd web && exec ./node_modules/.bin/vite --host 127.0.0.1 --port 5173 --strictPort >"$vite_log" 2>&1) &
vite_pid=$!
ready=""
for _ in $(seq 1 80); do
  if curl -fsS http://127.0.0.1:5173/app/ >/dev/null 2>&1; then ready=1; break; fi
  kill -0 "$vite_pid" 2>/dev/null || break
  sleep 0.25
done
[[ -n "$ready" ]] || fail "Vite did not become ready"

if [[ -n "$public_url" ]]; then
  app_url="$public_base/app/"
else
  cloudflared tunnel --url http://127.0.0.1:5173 --no-autoupdate >"$tunnel_log" 2>&1 &
  tunnel_pid=$!
  tunnel_url=""
  for _ in $(seq 1 120); do
    tunnel_url="$(grep -Eo 'https://[a-z0-9-]+\.trycloudflare\.com' "$tunnel_log" | head -1 || true)"
    [[ -n "$tunnel_url" ]] && break
    kill -0 "$tunnel_pid" 2>/dev/null || break
    sleep 0.25
  done
  [[ -n "$tunnel_url" ]] || fail "Cloudflare tunnel did not return a URL"

  # The URL becomes usable a moment after the connector registers. The Telegram
  # client resolves it independently, so a local DNS delay is not fatal.
  for _ in $(seq 1 60); do
    grep -q 'Registered tunnel connection' "$tunnel_log" && break
    kill -0 "$tunnel_pid" 2>/dev/null || break
    sleep 0.25
  done
  app_url="$tunnel_url/app/"
fi
menu_result="manual"
if MENU_TOKEN="$token" MENU_CHAT_ID="$chat_id" MENU_URL="$app_url" python3 - <<'PY'
import json
import os
import sys
import urllib.request

token = os.environ["MENU_TOKEN"]
chat_id = int(os.environ["MENU_CHAT_ID"])
url = os.environ["MENU_URL"]
payload = json.dumps({
    "chat_id": chat_id,
    "menu_button": {"type": "web_app", "text": "打开", "web_app": {"url": url}},
}).encode()
request = urllib.request.Request(
    f"https://api.telegram.org/bot{token}/setChatMenuButton",
    data=payload,
    headers={"Content-Type": "application/json"},
)
try:
    with urllib.request.urlopen(request, timeout=15) as response:
        result = json.load(response)
except Exception as exc:  # network/TLS; print no token
    print(f"menu button: failed ({type(exc).__name__})", file=sys.stderr)
    sys.exit(1)
if not result.get("ok"):
    print("menu button: Telegram rejected the update", file=sys.stderr)
    sys.exit(1)
print("menu button: updated for your chat")
PY
then
  menu_result="auto"
fi

cat <<EOF

Mini App ready.
  Local:  http://127.0.0.1:5173/app/
  HTTPS:  $app_url
  DB:     $DATABASE_PATH
  Logs:   $log_dir

Menu button: $menu_result (chat $chat_id)
Open the private chat with your bot and tap the menu button.
If it is not updated, run /setmenubutton in @BotFather and use the HTTPS URL above.
The default initData lifetime is 5 minutes; close and reopen the Mini App to refresh it.
Press Ctrl-C to stop everything.
EOF

wait "$vite_pid"
