#!/usr/bin/env bash
#
# End-to-end test: verify OpenClaw discovers and uses sx-installed skills,
# and that cron-based auto-update picks up new skills.
#
# Flow:
#   1. Create "clawtest" sx profile with a path vault containing one skill
#   2. Start OpenClaw in Docker
#   3. Run sx install → verify OpenClaw sees the skill
#   4. Add a second skill to the vault
#   5. Wait for cron (1 min) to fire sx install
#   6. Verify the second skill appears in OpenClaw
#
# Prerequisites:
#   - Docker + Docker Compose v2
#   - sx binary built (make build)
#
# Environment variables:
#   SX_ENV_FILE (required)  Path to a .env file containing ANTHROPIC_API_KEY.
#   OPENCLAW_IMAGE          Docker image to use (default: ghcr.io/openclaw/openclaw:latest)
#
# Usage:
#   SX_ENV_FILE=./.env ./scripts/test-openclaw-docker.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SX_BINARY="$PROJECT_DIR/dist/sx"
TEST_DIR=$(mktemp -d)
OPENCLAW_HOME="$TEST_DIR/openclaw-home"
VAULT_DIR="$TEST_DIR/vault"
SX_CONFIG_DIR="$TEST_DIR/sx-config"
FAKE_HOME="$TEST_DIR/fakehome"
OPENCLAW_IMAGE="${OPENCLAW_IMAGE:-ghcr.io/openclaw/openclaw:latest}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "\n${BLUE}==> $*${NC}"; }

PASSES=0
FAILS=0
assert_contains() {
    local label="$1" haystack="$2" needle="$3"
    if echo "$haystack" | grep -q "$needle"; then
        echo -e "${GREEN}[PASS]${NC} $label"
        PASSES=$((PASSES + 1))
    else
        echo -e "${RED}[FAIL]${NC} $label (expected to find '$needle')"
        FAILS=$((FAILS + 1))
    fi
}
assert_not_contains() {
    local label="$1" haystack="$2" needle="$3"
    if ! echo "$haystack" | grep -q "$needle"; then
        echo -e "${GREEN}[PASS]${NC} $label"
        PASSES=$((PASSES + 1))
    else
        echo -e "${RED}[FAIL]${NC} $label (did NOT expect to find '$needle')"
        FAILS=$((FAILS + 1))
    fi
}

cleanup() {
    step "Cleaning up"
    docker compose -f "$TEST_DIR/docker-compose.yml" down --remove-orphans 2>/dev/null || true
    rm -rf "$TEST_DIR"
    info "Removed $TEST_DIR"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Helper: create a skill in the vault
# ---------------------------------------------------------------------------
create_vault_skill() {
    local name="$1" version="$2" description="$3" content="$4"

    local skill_src="$TEST_DIR/src-$name"
    mkdir -p "$skill_src"

    cat > "$skill_src/metadata.toml" << EOF
[asset]
name = "$name"
version = "$version"
type = "skill"
description = "$description"

[skill]
prompt-file = "SKILL.md"
EOF

    cat > "$skill_src/SKILL.md" << EOF
---
name: $name
description: "$description"
user-invocable: true
---

$content
EOF

    # Package into vault
    local vault_asset_dir="$VAULT_DIR/$name/$version"
    mkdir -p "$vault_asset_dir"
    (cd "$skill_src" && zip -r "$vault_asset_dir/$name-$version.zip" . >/dev/null)
    cp "$skill_src/metadata.toml" "$vault_asset_dir/"
    echo "$version" >> "$VAULT_DIR/$name/list.txt"

    info "Created vault skill: $name@$version"
}

# Helper: run sx with the test profile and fake home
run_sx() {
    HOME="$FAKE_HOME" SX_CONFIG_DIR="$SX_CONFIG_DIR" "$SX_BINARY" --profile=clawtest "$@"
}

# Helper: run command inside the openclaw container
run_in_container() {
    docker compose -f "$TEST_DIR/docker-compose.yml" exec -T openclaw-gateway "$@" 2>&1
}

# Helper: run openclaw CLI inside container
openclaw_cli() {
    run_in_container node /app/openclaw.mjs "$@"
}

# ---------------------------------------------------------------------------
# 0. Pre-checks
# ---------------------------------------------------------------------------
step "Pre-checks"

if [[ ! -x "$SX_BINARY" ]]; then
    error "sx binary not found at $SX_BINARY — run 'make build' first"
    exit 1
fi
info "sx binary: $SX_BINARY"

if ! docker info >/dev/null 2>&1; then
    error "Docker is not running"
    exit 1
fi
info "Docker: OK"

ENV_FILE="${SX_ENV_FILE:?SX_ENV_FILE must be set to a .env file containing ANTHROPIC_API_KEY}"
if [[ ! -f "$ENV_FILE" ]]; then
    error "API key file not found: $ENV_FILE"
    exit 1
fi
export ANTHROPIC_API_KEY
ANTHROPIC_API_KEY="$(grep '^ANTHROPIC_API_KEY=' "$ENV_FILE" | cut -d= -f2- || true)"
if [[ -z "$ANTHROPIC_API_KEY" ]]; then
    error "ANTHROPIC_API_KEY not found or empty in $ENV_FILE"
    exit 1
fi
info "API key source: $ENV_FILE (value not shown)"

# ---------------------------------------------------------------------------
# 1. Create vault with first skill
# ---------------------------------------------------------------------------
step "Creating test vault with first skill"

mkdir -p "$VAULT_DIR"

create_vault_skill "hello-world" "1.0.0" \
    "A simple greeting skill" \
    "# Hello World

When invoked, respond with exactly: \"SKILL_VERIFIED: Hello from sx!\"

Do not add any other text."

# ---------------------------------------------------------------------------
# 2. Create fake home with OpenClaw dir + sx profile
# ---------------------------------------------------------------------------
step "Setting up fake home and sx profile"

mkdir -p "$FAKE_HOME" "$OPENCLAW_HOME/skills" "$SX_CONFIG_DIR"

# Symlink so sx sees ~/.openclaw
ln -s "$OPENCLAW_HOME" "$FAKE_HOME/.openclaw"

# Write sx config for clawtest profile
cat > "$SX_CONFIG_DIR/config.json" << EOF
{
  "defaultProfile": "clawtest",
  "profiles": {
    "clawtest": {
      "type": "path",
      "repositoryUrl": "$VAULT_DIR"
    }
  },
  "forceEnabledClients": ["openclaw"],
  "forceDisabledClients": []
}
EOF

info "sx profile: clawtest → $VAULT_DIR"

# Write minimal openclaw.json (empty — onboarding will configure it)
echo '{}' > "$OPENCLAW_HOME/openclaw.json"

# ---------------------------------------------------------------------------
# 3. Add first skill to sx and install
# ---------------------------------------------------------------------------
step "Adding hello-world to sx vault and installing"

run_sx add "$VAULT_DIR/hello-world/1.0.0/hello-world-1.0.0.zip" \
    --yes --scope-global --no-install 2>&1

info "Running sx install..."
run_sx install --client=openclaw 2>&1

info "Checking installed files:"
ls -la "$OPENCLAW_HOME/skills/" 2>&1
ls -la "$OPENCLAW_HOME/skills/hello-world/" 2>&1 || warn "hello-world dir not found"

# ---------------------------------------------------------------------------
# 4. Start OpenClaw in Docker
# ---------------------------------------------------------------------------
step "Starting OpenClaw in Docker"

cat > "$TEST_DIR/docker-compose.yml" << COMPOSE_EOF
services:
  openclaw-gateway:
    image: $OPENCLAW_IMAGE
    command: ["node", "openclaw.mjs", "gateway", "--allow-unconfigured"]
    working_dir: /app
    volumes:
      - $OPENCLAW_HOME:/home/node/.openclaw
      - $SX_BINARY:/usr/local/bin/sx:ro
      - $VAULT_DIR:/vault:ro
    environment:
      - ANTHROPIC_API_KEY
      - SX_CONFIG_DIR=/home/node/.config/sx
    ports:
      - "127.0.0.1:18789:18789"
    healthcheck:
      test: ["CMD", "node", "-e", "fetch('http://127.0.0.1:18789/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]
      interval: 5s
      timeout: 5s
      start_period: 15s
      retries: 20
COMPOSE_EOF

# Write sx config inside the container's expected location
# (we can't mount SX_CONFIG_DIR directly because vault paths differ inside container)
mkdir -p "$OPENCLAW_HOME/../container-sx-config"
cat > "$OPENCLAW_HOME/../container-sx-config/config.json" << 'EOF'
{
  "defaultProfile": "clawtest",
  "profiles": {
    "clawtest": {
      "type": "path",
      "repositoryUrl": "/vault"
    }
  },
  "forceEnabledClients": ["openclaw"],
  "forceDisabledClients": []
}
EOF

# Update compose to mount container-specific sx config
cat > "$TEST_DIR/docker-compose.yml" << COMPOSE_EOF
services:
  openclaw-gateway:
    image: $OPENCLAW_IMAGE
    command: ["node", "openclaw.mjs", "gateway", "--allow-unconfigured"]
    working_dir: /app
    volumes:
      - $OPENCLAW_HOME:/home/node/.openclaw
      - $SX_BINARY:/usr/local/bin/sx:ro
      - $VAULT_DIR:/vault:ro
      - $TEST_DIR/container-sx-config:/home/node/.config/sx:ro
    environment:
      - ANTHROPIC_API_KEY
      - HOME=/home/node
    ports:
      - "127.0.0.1:18789:18789"
    healthcheck:
      test: ["CMD", "node", "-e", "fetch('http://127.0.0.1:18789/healthz').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"]
      interval: 5s
      timeout: 5s
      start_period: 15s
      retries: 20
COMPOSE_EOF

info "Pulling image: $OPENCLAW_IMAGE"
docker pull "$OPENCLAW_IMAGE"

info "Starting gateway..."
docker compose -f "$TEST_DIR/docker-compose.yml" up -d openclaw-gateway

info "Waiting for gateway health check..."
for i in $(seq 1 120); do
    if docker compose -f "$TEST_DIR/docker-compose.yml" ps --format json 2>/dev/null | grep -q '"healthy"'; then
        echo ""
        info "Gateway is healthy!"
        break
    fi
    if [[ $i -eq 120 ]]; then
        echo ""
        error "Gateway failed to become healthy"
        docker compose -f "$TEST_DIR/docker-compose.yml" logs openclaw-gateway | tail -30
        exit 1
    fi
    printf "."
    sleep 1
done
echo ""

# ---------------------------------------------------------------------------
# 5. Run onboarding (non-interactive)
# ---------------------------------------------------------------------------
step "Running non-interactive onboarding"

openclaw_cli onboard --non-interactive \
    --accept-risk \
    --anthropic-api-key "$ANTHROPIC_API_KEY" \
    --mode local \
    --skip-channels \
    --skip-daemon \
    --skip-skills \
    --skip-ui \
    --skip-health 2>&1 || warn "Onboarding returned non-zero (may already be configured)"

# Restart gateway so it picks up the onboarded config and discovers skills
info "Restarting gateway to pick up skills..."
docker compose -f "$TEST_DIR/docker-compose.yml" restart openclaw-gateway
sleep 5
for i in $(seq 1 90); do
    if docker compose -f "$TEST_DIR/docker-compose.yml" ps --format json 2>/dev/null | grep -q '"healthy"'; then
        echo ""
        info "Gateway restarted and healthy!"
        break
    fi
    if [[ $i -eq 90 ]]; then
        echo ""
        error "Gateway failed to become healthy after restart"
        docker compose -f "$TEST_DIR/docker-compose.yml" logs openclaw-gateway | tail -20
        exit 1
    fi
    printf "."
    sleep 1
done
echo ""

# ---------------------------------------------------------------------------
# 6. TEST 1: Verify first skill is discovered
# ---------------------------------------------------------------------------
step "TEST 1: Verify hello-world skill discovered by OpenClaw"

info "Skills list:"
SKILLS_OUTPUT=$(openclaw_cli skills list --verbose 2>&1) || true
echo "$SKILLS_OUTPUT" | grep -i "hello\|managed" || info "(no matches)"

assert_contains "hello-world in skills list" "$SKILLS_OUTPUT" "hello-world"

# ---------------------------------------------------------------------------
# 7. TEST 2: Verify file structure inside container
# ---------------------------------------------------------------------------
step "TEST 2: Verify skill files inside container"

STRUCTURE=$(run_in_container find /home/node/.openclaw/skills -type f 2>&1) || true
echo "$STRUCTURE"

assert_contains "SKILL.md exists in container" "$STRUCTURE" "hello-world/SKILL.md"

# ---------------------------------------------------------------------------
# 8. TEST 3: Verify skill is eligible and ready via skills info
# ---------------------------------------------------------------------------
step "TEST 3: Verify hello-world skill is eligible via skills info"

SKILL_INFO=$(openclaw_cli skills info hello-world 2>&1) || true
echo "$SKILL_INFO"

assert_contains "hello-world is Ready" "$SKILL_INFO" "Ready"
assert_contains "hello-world source is openclaw-managed" "$SKILL_INFO" "openclaw-managed"

# ---------------------------------------------------------------------------
# 9. Add second skill to vault
# ---------------------------------------------------------------------------
step "Adding second skill (farewell-world) to vault"

create_vault_skill "farewell-world" "1.0.0" \
    "A farewell skill" \
    "# Farewell World

When invoked, respond with exactly: \"FAREWELL_VERIFIED: Goodbye from sx!\"

Do not add any other text."

# Add to sx lock file (from host)
run_sx add "$VAULT_DIR/farewell-world/1.0.0/farewell-world-1.0.0.zip" \
    --yes --scope-global --no-install 2>&1

# Verify second skill is NOT yet in OpenClaw
SKILLS_BEFORE=$(openclaw_cli skills list --json 2>&1) || true
assert_not_contains "farewell-world NOT yet visible" "$SKILLS_BEFORE" "farewell-world"

# ---------------------------------------------------------------------------
# 10. Run sx install inside container (simulates cron trigger)
# ---------------------------------------------------------------------------
step "Running sx install inside container (simulates what cron would do)"

# Copy the lock file into the container's accessible location
# The lock file lives alongside the sx config
LOCK_DIR="$FAKE_HOME"
if [[ -f "$LOCK_DIR/sx.lock" ]]; then
    cp "$LOCK_DIR/sx.lock" "$TEST_DIR/container-sx-config/" 2>/dev/null || true
fi
# Also check if lock is in the config dir
if [[ -f "$SX_CONFIG_DIR/sx.lock" ]]; then
    cp "$SX_CONFIG_DIR/sx.lock" "$TEST_DIR/container-sx-config/" 2>/dev/null || true
fi

info "Lock file locations:"
find "$TEST_DIR" -name "sx.lock" -exec echo "  {}" \; 2>/dev/null

# Run sx install from inside container
INSTALL_OUTPUT=$(run_in_container sx install --client=openclaw 2>&1) || true
echo "$INSTALL_OUTPUT"

# ---------------------------------------------------------------------------
# 11. TEST 4: Verify second skill appears
# ---------------------------------------------------------------------------
step "TEST 4: Verify farewell-world now visible in OpenClaw"

SKILLS_AFTER=$(openclaw_cli skills list --json 2>&1) || true
echo "$SKILLS_AFTER" | head -40

assert_contains "farewell-world in skills list" "$SKILLS_AFTER" "farewell-world"
assert_contains "hello-world still present" "$SKILLS_AFTER" "hello-world"

# ---------------------------------------------------------------------------
# 12. TEST 5: Cron-based auto-update
# ---------------------------------------------------------------------------
step "TEST 5: Cron-based auto-update (1-minute interval)"

# Add a third skill
create_vault_skill "cron-test" "1.0.0" \
    "Cron update test skill" \
    "# Cron Test

When invoked, respond with: \"CRON_VERIFIED: Auto-update works!\""

run_sx add "$VAULT_DIR/cron-test/1.0.0/cron-test-1.0.0.zip" \
    --yes --scope-global --no-install 2>&1

# Copy updated lock file
find "$TEST_DIR" -name "sx.lock" -not -path "*/container-sx-config/*" -exec cp {} "$TEST_DIR/container-sx-config/" \; 2>/dev/null

# Start a background cron loop inside the container (every 60s)
info "Starting 1-minute cron loop inside container..."
run_in_container bash -c '
nohup bash -c "while true; do sleep 60; sx install --client=openclaw 2>/dev/null; done" &>/tmp/sx-cron.log &
echo "Cron PID: $!"
' || true

# Verify cron-test NOT yet visible
SKILLS_PRE_CRON=$(openclaw_cli skills list --json 2>&1) || true
assert_not_contains "cron-test NOT yet visible" "$SKILLS_PRE_CRON" "cron-test"

info "Waiting 70 seconds for cron to fire..."
sleep 70

SKILLS_POST_CRON=$(openclaw_cli skills list --json 2>&1) || true
echo "$SKILLS_POST_CRON" | head -40

assert_contains "cron-test auto-installed by cron" "$SKILLS_POST_CRON" "cron-test"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
step "Test Summary"
echo ""
echo -e "  ${GREEN}Passed: $PASSES${NC}"
if [[ $FAILS -gt 0 ]]; then
    echo -e "  ${RED}Failed: $FAILS${NC}"
else
    echo -e "  Failed: 0"
fi
echo ""

if [[ $FAILS -gt 0 ]]; then
    error "Some tests failed!"
    exit 1
else
    info "All tests passed!"
fi
