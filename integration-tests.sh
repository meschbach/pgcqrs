#!/bin/bash

set -e

# --- Tunables ------------------------------------------------------------------
READINESS_TIMEOUT_S=15      # startup gate deadline (both modes)
READINESS_POLL_S=0.25       # readiness probe interval
TEARDOWN_CAP_S=2            # per-wait cap for service death / dump flush
FAIL_TAIL_LINES=40          # failing-suite console tail size
DUMP_TAIL_LINES=200         # goroutines.txt fallback tail size
GO_TEST_TIMEOUT=5s          # per-suite go test --timeout

MODE="${1:-local}"
INTEGRATION_RUN_COUNT="${INTEGRATION_RUN_COUNT:-1}"
SERVER_PID=""
SERVER_LOG=""

case "$MODE" in
    local|docked) ;;
    *)
        echo "Unknown mode: $MODE"
        echo "Usage: $0 [local|docked]"
        exit 1
        ;;
esac

# --- Repository anchoring and diagnostics directory ------------------------------
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$REPO_ROOT" ]; then
    REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi

DIAGNOSTICS_BASE="${PGCQRS_TEST_DIAG_DIR:-$REPO_ROOT/run/test-diagnostics}"
RUN_ID="$(date -u +%Y%m%d-%H%M%S)-$MODE-$$"
DIAGNOSTICS_DIR="$DIAGNOSTICS_BASE/$RUN_ID"
mkdir -p "$DIAGNOSTICS_BASE"
if ! mkdir "$DIAGNOSTICS_DIR" 2>/dev/null; then
    echo "ERROR: diagnostics directory already exists: $DIAGNOSTICS_DIR" >&2
    exit 1
fi
SERVER_LOG="$DIAGNOSTICS_DIR/server.log"
SUMMARY_FILE="$DIAGNOSTICS_DIR/summary.txt"

# --- Run summary -----------------------------------------------------------------
summary() {
    printf '%s\n' "$1" >>"$SUMMARY_FILE" 2>/dev/null || true
}

fail_run() {
    summary "verdict: FAILURE ($1)"
    exit 1
}

# Record a startup failure in the summary, announce it on the console, point at
# any server-owned evidence, and stop before any transport suite runs.
report_startup_failure() {
    summary "startup failure: $1"
    summary "verdict: FAILURE (startup)"
    echo "ERROR: $2"
    if [ -n "$SERVER_LOG" ]; then
        echo "Server log: $SERVER_LOG"
    fi
    exit 1
}

# --- Readiness gating -------------------------------------------------------------
# Poll GET /ops/readiness until a literal HTTP 200 or the deadline; connection
# success alone is insufficient. With an owned pid given, its death aborts the
# poll immediately (return code 2) so a stale instance answering 200 on the same
# port can never gate the run.
await_ready() {
    local base_url="$1" owned_pid="${2:-}"
    local deadline=$(( $(date +%s) + READINESS_TIMEOUT_S ))
    while :; do
        if [ -n "$owned_pid" ] && ! kill -0 "$owned_pid" 2>/dev/null; then
            return 2
        fi
        if curl -fsS --max-time 1 "$base_url/ops/readiness" >/dev/null 2>&1; then
            return 0
        fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
            return 1
        fi
        sleep "$READINESS_POLL_S"
    done
}

# Poll until the process dies or the cap elapses; non-zero means still alive.
await_death() {
    local pid="$1" cap_seconds="$2"
    local deadline=$(( $(date +%s) + cap_seconds ))
    while kill -0 "$pid" 2>/dev/null; do
        if [ "$(date +%s)" -ge "$deadline" ]; then
            return 1
        fi
        sleep 0.1
    done
    return 0
}

# --- Transport suites ---------------------------------------------------------------
run_transport() {
    local label="$1"
    local out_file="$DIAGNOSTICS_DIR/test-$label.out"
    local start end dur rc
    case "$label" in
        memory)
            export PGCQRS_TEST_TRANSPORT="memory"
            export PGCQRS_TEST_URL="$HTTP_URL"
            ;;
        http)
            # unset-vs-set stays distinct from an explicit value by design
            unset PGCQRS_TEST_TRANSPORT
            export PGCQRS_TEST_URL="$HTTP_URL"
            ;;
        grpc)
            export PGCQRS_TEST_TRANSPORT="grpc"
            export PGCQRS_TEST_URL="$GRPC_URL"
            ;;
    esac
    export PGCQRS_TEST_APP_BASE="systest-"

    start="$(date +%s)"
    rc=0
    # INTEGRATION_RUN_OPTS is intentionally unquoted: word splitting delivers flags.
    (cd "$REPO_ROOT" && go test -count=$INTEGRATION_RUN_COUNT $INTEGRATION_RUN_OPTS --timeout "$GO_TEST_TIMEOUT" ./systest/...) >"$out_file" 2>&1 || rc=$?
    end="$(date +%s)"
    dur=$(( end - start ))
    if [ "$rc" -eq 0 ]; then
        echo "[$label] PASS ${dur}s"
        summary "phase $label: PASS (${dur}s)"
    else
        echo "[$label] FAIL ${dur}s"
        summary "phase $label: FAIL (${dur}s)"
        echo "--- last $FAIL_TAIL_LINES lines of $out_file ---"
        tail -n "$FAIL_TAIL_LINES" "$out_file" || true
        echo "--- full output: $out_file ---"
    fi
    return "$rc"
}

# --- Local teardown: post-mortem dump extraction --------------------------------------
extract_goroutines() {
    local dump="$DIAGNOSTICS_DIR/goroutines.txt"
    if [ ! -f "$SERVER_LOG" ]; then
        printf '# server.log not found; no dump available\n' >"$dump" 2>/dev/null || true
        return 0
    fi
    awk '/^SIGQUIT: quit$/ {on=1} on' "$SERVER_LOG" >"$dump" 2>/dev/null || true
    if [ ! -s "$dump" ]; then
        {
            printf '# no SIGQUIT marker found; tail of server.log\n'
            tail -n "$DUMP_TAIL_LINES" "$SERVER_LOG" 2>/dev/null || true
        } >"$dump"
    fi
}

teardown_local_service() {
    local status="$1"
    # failure/hang => SIGQUIT dump material; all-green => quiet plain kill
    if [ "$status" -ne 0 ]; then
        kill -QUIT "$SERVER_PID" 2>/dev/null || true
    else
        kill "$SERVER_PID" 2>/dev/null || true
    fi
    if ! await_death "$SERVER_PID" "$TEARDOWN_CAP_S"; then
        kill -KILL "$SERVER_PID" 2>/dev/null || true
        await_death "$SERVER_PID" "$TEARDOWN_CAP_S" || true
    fi
    # reap quietly once death is confirmed; never block on a stuck process
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    if [ "$status" -ne 0 ]; then
        extract_goroutines
    fi
}

# --- Docked failure-path diagnostics ----------------------------------------------------
capture_docked_diagnostics() {
    local ids count=0 candidates="" id running created class chosen chosen_class chosen_running
    ids="$(docker ps -aq --filter label=com.docker.compose.service=pgcqrs 2>/dev/null || true)"
    if [ -z "$ids" ]; then
        summary "container: no service container found"
        return 0
    fi
    for id in $ids; do
        count=$((count + 1))
        running="$(docker inspect -f '{{.State.Running}}' "$id" 2>/dev/null || echo unknown)"
        created="$(docker inspect -f '{{.Created}}' "$id" 2>/dev/null || echo unknown)"
        # candidate lines: "<class> <created> <id>"; RFC3339 sorts lexically.
        # Class is two-tier (1=running, 0=everything else) so an unreadable
        # state cannot outrank a readable one -- matching inspect-then-branch
        # semantics rather than raw string order.
        class=0
        [ "$running" = "true" ] && class=1
        candidates="${candidates}${class} ${created} ${id}
"
    done
    # newest running first, then newest of the rest (reversed sort on both keys)
    set -- $(printf '%s' "$candidates" | sort -k1,1r -k2,2r | head -n 1)
    local chosen="$3" chosen_class="$1"
    local chosen_running="false"
    [ "$chosen_class" = "1" ] && chosen_running="true"
    if [ "$count" -gt 1 ]; then
        echo "WARNING: multiple service containers matched label filter; using $chosen"
        summary "matched containers:"
        printf '%s' "$candidates" | while read -r running created id; do
            summary "  $id (running=$running)"
        done
    fi
    if [ "$chosen_running" = "true" ]; then
        docker kill --signal=QUIT "$chosen" >/dev/null 2>&1 || true
        # bounded wait for dump flush / container exit
        local deadline=$(( $(date +%s) + TEARDOWN_CAP_S ))
        while [ "$(docker inspect -f '{{.State.Running}}' "$chosen" 2>/dev/null || echo false)" = "true" ] && [ "$(date +%s)" -lt "$deadline" ]; do
            sleep 0.1
        done
    fi
    docker logs --timestamps "$chosen" >"$DIAGNOSTICS_DIR/docker-service.log" 2>&1 || true
    # Docker ignores restart policies after manual kills, so restore is mandatory (best effort).
    running="$(docker inspect -f '{{.State.Running}}' "$chosen" 2>/dev/null || echo false)"
    if [ "$running" != "true" ]; then
        if ! docker start "$chosen" >/dev/null 2>&1; then
            echo "WARNING: failed to restore container $chosen via docker start"
            summary "container restore: FAILED"
        fi
    fi
}

# --- Teardown dispatch: sole owner of service signaling and dump extraction --------------
on_exit() {
    local status=$?
    set +e
    if [ "$MODE" = "local" ] && [ -n "$SERVER_PID" ]; then
        teardown_local_service "$status"
    elif [ "$MODE" = "docked" ] && [ "$status" -ne 0 ]; then
        capture_docked_diagnostics
    fi
    echo "Diagnostics: $DIAGNOSTICS_DIR"
    exit "$status"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# === Main ================================================================================
summary "mode: $MODE"
summary "timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
summary "git sha: $(git rev-parse HEAD 2>/dev/null || echo unknown)"
summary "go version: $(go version 2>/dev/null | awk '{print $3}' || echo unknown)"

case "$MODE" in
    local)
        echo "=== Starting local service ==="
        "$REPO_ROOT/service" serve 2>"$SERVER_LOG" &
        SERVER_PID=$!
        # Gate and suites must agree on where the service lives: derive all URLs
        # from the service's own resolution instead of hardcoding ports.
        LISTEN_ADDR="${PGCQRS_LISTENER_ADDRESS:-localhost:9000}"
        HTTP_URL="http://${LISTEN_ADDR}"
        READY_URL="${HTTP_URL}"
        GRPC_URL="${PGCQRS_GRPC_LISTENER_ADDRESS:-localhost:9001}"
        # 0.0.0.0 is a fine bind default but not a portable dial target
        case "$GRPC_URL" in
            0.0.0.0:*) GRPC_URL="127.0.0.1:${GRPC_URL#0.0.0.0:}" ;;
        esac
        rc=0
        await_ready "$READY_URL" "$SERVER_PID" || rc=$?
        if [ "$rc" -eq 2 ]; then
            report_startup_failure \
                "service process died during startup" \
                "local service died during startup"
        fi
        if [ "$rc" -ne 0 ]; then
            report_startup_failure \
                "service did not become ready within deadline" \
                "local service did not become ready within deadline"
        fi
        ;;
    docked)
        echo "=== Using docked service ==="
        HTTP_URL="http://localhost:26000"
        GRPC_URL="localhost:26001"
        if ! await_ready "$HTTP_URL"; then
            report_startup_failure \
                "docked service did not become ready" \
                "docked service did not become ready at $HTTP_URL within deadline"
        fi
        ;;
esac

run_transport memory || fail_run "memory transport failed"
run_transport http || fail_run "http transport failed"
run_transport grpc || fail_run "grpc transport failed"

summary "verdict: SUCCESS"
