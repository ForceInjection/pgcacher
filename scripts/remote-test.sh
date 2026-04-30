#!/usr/bin/env bash
#
# remote-test.sh - Cross-compile pgcacher on macOS, deploy to a remote Linux
# server via SSH, and run smoke tests (help, file probe, optional -container).
#
# Configure the target via env vars (no defaults, to avoid leaking credentials):
#   REMOTE_HOST   target host/IP                 (required)
#   REMOTE_USER   SSH username                   (default: current $USER)
#   REMOTE_PASS   SSH password for sshpass       (optional; prefer key auth)
#   REMOTE_BASE   base dir on remote             (default: ~REMOTE_USER)
#   REMOTE_DIR    explicit test dir              (default: $REMOTE_BASE/pgcacher-test-<ts>)
#
# Example:
#   REMOTE_HOST=10.0.0.5 REMOTE_USER=ubuntu REMOTE_PASS=xxxxx \
#       scripts/remote-test.sh
#
# Requirements (on local macOS):
#   - Go toolchain (for cross-compile)
#   - sshpass (for password auth): brew install hudochenkov/sshpass/sshpass
#     Alternative: set up key auth first with `ssh-copy-id $REMOTE_USER@$REMOTE_HOST`
#     and the script will use plain ssh/scp without a password.

set -euo pipefail

if [[ -z "${REMOTE_HOST:-}" ]]; then
    echo "error: REMOTE_HOST is required. Example: REMOTE_HOST=10.0.0.5 $0" >&2
    exit 2
fi

REMOTE_USER="${REMOTE_USER:-${USER}}"
REMOTE_PASS="${REMOTE_PASS:-}"
REMOTE_BASE="${REMOTE_BASE:-/home/${REMOTE_USER}}"
REMOTE_DIR="${REMOTE_DIR:-${REMOTE_BASE}/pgcacher-test-$(date +%Y%m%d-%H%M%S)}"

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_NAME="pgcacher"
BIN_PATH="${PROJECT_ROOT}/${BIN_NAME}"
TARGET="${REMOTE_USER}@${REMOTE_HOST}"

log() { printf '\033[1;34m[remote-test]\033[0m %s\n' "$*"; }

# Prefer sshpass (password auth) only if both sshpass is installed AND a
# password was supplied via REMOTE_PASS. Otherwise fall back to plain ssh/scp
# which requires prior key auth setup. SSHPASS env var is safer than the -p
# flag (password does not appear in process list).
if command -v sshpass >/dev/null 2>&1 && [[ -n "$REMOTE_PASS" ]]; then
    export SSHPASS="$REMOTE_PASS"
    # PreferredAuthentications=password + PubkeyAuthentication=no forces password
    # auth, avoiding scp/ssh consuming retries on pubkey/gssapi before sshpass
    # injects the password (common macOS issue).
    SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o PreferredAuthentications=password -o PubkeyAuthentication=no)
    SSH_CMD=(sshpass -e ssh "${SSH_OPTS[@]}")
    SCP_CMD=(sshpass -e scp "${SSH_OPTS[@]}")
else
    log "using plain ssh (REMOTE_PASS unset or sshpass missing); requires key auth."
    log "  set up keys once: ssh-copy-id ${TARGET}"
    SSH_CMD=(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)
    SCP_CMD=(scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)
fi

log "1/4 cross-compile pgcacher for linux/amd64 ..."
(
    cd "$PROJECT_ROOT"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o "$BIN_NAME" .
)
ls -lh "$BIN_PATH"

log "2/4 prepare remote dir ${REMOTE_DIR} ..."
"${SSH_CMD[@]}" "$TARGET" "mkdir -p '$REMOTE_DIR' && echo 'remote dir ready: ' \$(pwd)"

log "3/4 upload binary to ${TARGET}:${REMOTE_DIR}/ ..."
"${SCP_CMD[@]}" "$BIN_PATH" "${TARGET}:${REMOTE_DIR}/"

log "4/4 run smoke tests on ${TARGET} ..."
"${SSH_CMD[@]}" "$TARGET" bash -s <<REMOTE_EOF
set -e
cd "${REMOTE_DIR}"

echo '=== [A] host info ==='
uname -a
cat /etc/os-release 2>/dev/null | head -3 || true
echo

echo '=== [B] pgcacher binary info ==='
file ./pgcacher 2>/dev/null || true
ls -lh ./pgcacher
echo

echo '=== [C] pgcacher -h ==='
./pgcacher -h 2>&1 | head -40 || true
echo

echo '=== [D] probe /bin/bash page cache ==='
./pgcacher /bin/bash
echo

echo '=== [E] top-5 cached files across all processes (requires root) ==='
./pgcacher -top -limit 5 || echo '(top mode failed; may need root)'
echo

if command -v docker >/dev/null 2>&1; then
    echo '=== [F] docker containers ==='
    docker ps --format 'table {{.ID}}\t{{.Image}}\t{{.Names}}' | head -10
    CID=\$(docker ps -q | head -1 || true)
    if [ -n "\$CID" ]; then
        FULL=\$(docker inspect --format '{{.Id}}' "\$CID")
        echo
        echo "=== [G] pgcacher -container \$FULL (top 5) ==="
        ./pgcacher -container "\$FULL" -top -limit 5 -verbose || \
            echo '(-container test failed; check cgroup layout / permissions)'
    else
        echo '(no running containers; skipping -container test)'
    fi
else
    echo '(docker not installed; skipping -container test)'
fi

echo
echo '=== done ==='
REMOTE_EOF

log "artifacts kept at ${TARGET}:${REMOTE_DIR}"
log "to clean up: ${SSH_CMD[*]} ${TARGET} \"rm -rf '${REMOTE_DIR}'\""
