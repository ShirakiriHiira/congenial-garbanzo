#!/usr/bin/env bash
# Local test mode, without Telegram credentials:
#   ./scripts/test-miniapp.sh
# Open http://127.0.0.1:5180/app/; Ctrl-C removes the session database.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
node_major() { "$1" -p 'process.versions.node.split(".")[0]' 2>/dev/null || true; }
node_bin=""
if [[ -n "${MINIAPP_NODE_BIN:-}" ]]; then
  node_bin="$(command -v -- "$MINIAPP_NODE_BIN" || true)"
  [[ -n "$node_bin" && "$(node_major "$node_bin")" == "24" ]] || {
    echo "error: MINIAPP_NODE_BIN must point to an executable Node 24 binary" >&2
    exit 1
  }
elif command -v node >/dev/null && [[ "$(node_major node)" == "24" ]]; then
  node_bin="$(command -v node)"
else
  node_arch=""
  case "$(uname -s):$(uname -m)" in
    Linux:x86_64) node_arch="linux-x64" ;;
    Linux:aarch64|Linux:arm64) node_arch="linux-arm64" ;;
  esac
  cached_node="$root/.cache/node-v24.22.0-$node_arch/bin/node"
  if [[ -n "$node_arch" && -x "$cached_node" && "$(node_major "$cached_node")" == "24" ]]; then
    node_bin="$cached_node"
  else
    echo "error: install Node 24 on PATH or set MINIAPP_NODE_BIN=/absolute/path/to/node (Node 24)" >&2
    exit 1
  fi
fi
[[ "$node_bin" == /* ]] || node_bin="$root/$node_bin"
export PATH="$(dirname "$node_bin"):$PATH"
command -v npm >/dev/null || {
  echo "error: npm is required; install npm for the selected Node 24 runtime" >&2
  exit 1
}

for port in 8088 5180; do
  if ss -ltn 2>/dev/null | awk '{print $4}' | grep -q ":$port$"; then
    echo "error: port $port is already in use; stop the existing process first" >&2
    exit 1
  fi
done

run_dir="$(mktemp -d "${TMPDIR:-/tmp}/miniapp-test-launcher.XXXXXX")"
api_pid=""
vite_pid=""
cleanup() {
  trap - EXIT INT TERM
  for pid in "$vite_pid" "$api_pid"; do
    [[ -z "$pid" ]] || kill "$pid" 2>/dev/null || true
  done
  for pid in "$vite_pid" "$api_pid"; do
    [[ -z "$pid" ]] || wait "$pid" 2>/dev/null || true
  done
  rm -rf "$run_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
  echo "error: $*" >&2
  for file in "$run_dir/api.log" "$run_dir/vite.log"; do
    [[ ! -s "$file" ]] || cat "$file" >&2
  done
  exit 1
}

echo "Installing frontend dependencies…"
(cd web && npm ci --silent) || fail "npm ci failed"

echo "Building isolated test API…"
GOTOOLCHAIN=go1.27.3+auto go build -trimpath -o "$run_dir/miniapp-test" ./cmd/miniapp-test || fail "Go build failed"
"$run_dir/miniapp-test" >"$run_dir/api.log" 2>&1 &
api_pid=$!
ready=""
for ((attempt=0; attempt<100; attempt++)); do
  kill -0 "$api_pid" 2>/dev/null || break
  if curl -fsS http://127.0.0.1:8088/__test/init-data >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.1
done
[[ -n "$ready" ]] || fail "test API did not start on 127.0.0.1:8088"
kill -0 "$api_pid" 2>/dev/null || fail "test API stopped"

(cd web && exec env VITE_MINIAPP_TEST_MODE=true "$node_bin" ./node_modules/vite/bin/vite.js --host 127.0.0.1 --port 5180 --strictPort >"$run_dir/vite.log" 2>&1) &
vite_pid=$!
ready=""
for ((attempt=0; attempt<100; attempt++)); do
  kill -0 "$vite_pid" 2>/dev/null || break
  if curl -fsS http://127.0.0.1:5180/app/ >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.1
done
[[ -n "$ready" ]] || fail "Vite did not start on 127.0.0.1:5180"
kill -0 "$vite_pid" 2>/dev/null || fail "Vite stopped"

printf '\nTEST MODE: http://127.0.0.1:5180/app/\nSeeded data persists until you stop this command. Restart to reset.\nAPI: http://127.0.0.1:8088 (loopback only)\nPress Ctrl-C to stop and remove the temporary database.\n\n'
wait -n "$api_pid" "$vite_pid" || fail "a test server stopped"
fail "a test server stopped"
