#!/bin/bash
# Inject fake edit war data into Redis for visual testing
set -e

REDIS="docker exec wikisurge-redis redis-cli"
NOW=$(date +%s)

###############################################################################
# 1. US ELECTION
###############################################################################
PAGE="2024 United States presidential election"

# Timeline
$REDIS DEL "editwar:timeline:$PAGE" > /dev/null 2>&1 || true
$REDIS RPUSH "editwar:timeline:$PAGE" \
  "{\"user\":\"MAGA_patriot\",\"timestamp\":$((NOW-7200)),\"byte_change\":2500,\"comment\":\"Added election fraud evidence\"}" \
  "{\"user\":\"FactChecker99\",\"timestamp\":$((NOW-6600)),\"byte_change\":-2400,\"comment\":\"Removed unsourced conspiracy claims\"}" \
  "{\"user\":\"MAGA_patriot\",\"timestamp\":$((NOW-6000)),\"byte_change\":1800,\"comment\":\"Restored with court documents\"}" \
  "{\"user\":\"BidenSupporter\",\"timestamp\":$((NOW-5400)),\"byte_change\":-1600,\"comment\":\"Reverted - docs are fabricated\"}" \
  "{\"user\":\"BalancedView\",\"timestamp\":$((NOW-4800)),\"byte_change\":600,\"comment\":\"Added neutral summary of disputes\"}" \
  "{\"user\":\"MAGA_patriot\",\"timestamp\":$((NOW-4200)),\"byte_change\":1200,\"comment\":\"Re-added documented irregularities\"}" \
  "{\"user\":\"FactChecker99\",\"timestamp\":$((NOW-3600)),\"byte_change\":-1000,\"comment\":\"Reverted per WP:RS\"}" \
  "{\"user\":\"TrumpTrain2024\",\"timestamp\":$((NOW-3000)),\"byte_change\":900,\"comment\":\"Added state-level analysis\"}" \
  "{\"user\":\"BidenSupporter\",\"timestamp\":$((NOW-2400)),\"byte_change\":-850,\"comment\":\"Removed original research\"}" \
  "{\"user\":\"MAGA_patriot\",\"timestamp\":$((NOW-1800)),\"byte_change\":1500,\"comment\":\"Restored sourced material\"}" \
  "{\"user\":\"FactChecker99\",\"timestamp\":$((NOW-1200)),\"byte_change\":-1400,\"comment\":\"Third revert - seeking admin\"}" \
  "{\"user\":\"TrumpTrain2024\",\"timestamp\":$((NOW-600)),\"byte_change\":800,\"comment\":\"Added polling data\"}" > /dev/null

# Editors
$REDIS DEL "editwar:editors:$PAGE" > /dev/null 2>&1 || true
$REDIS HSET "editwar:editors:$PAGE" \
  "MAGA_patriot" "4" \
  "FactChecker99" "3" \
  "BidenSupporter" "2" \
  "TrumpTrain2024" "2" \
  "BalancedView" "1" > /dev/null

# Changes  
$REDIS DEL "editwar:changes:$PAGE" > /dev/null 2>&1 || true
$REDIS RPUSH "editwar:changes:$PAGE" "2500" "-2400" "1800" "-1600" "600" "1200" "-1000" "900" "-850" "1500" "-1400" "800" > /dev/null

# Marker key (required for GetActiveEditWars to discover this war)
$REDIS SET "editwar:$PAGE" "1" EX 43200 > /dev/null

# Start time & server URL
$REDIS SET "editwar:start:$PAGE" "$(date -d @$((NOW-7200)) -u +%Y-%m-%dT%H:%M:%SZ)" EX 43200 > /dev/null
$REDIS SET "editwar:serverurl:$PAGE" "https://en.wikipedia.org" EX 43200 > /dev/null

# TTLs
$REDIS EXPIRE "editwar:timeline:$PAGE" 43200 > /dev/null
$REDIS EXPIRE "editwar:editors:$PAGE" 43200 > /dev/null
$REDIS EXPIRE "editwar:changes:$PAGE" 43200 > /dev/null

echo "✓ Injected: $PAGE (12 edits, 5 editors)"

###############################################################################
# 2. RUSSIA-UKRAINE
###############################################################################
PAGE="Russian invasion of Ukraine"

$REDIS DEL "editwar:timeline:$PAGE" > /dev/null 2>&1 || true
$REDIS RPUSH "editwar:timeline:$PAGE" \
  "{\"user\":\"Kremlin_watcher\",\"timestamp\":$((NOW-5400)),\"byte_change\":3000,\"comment\":\"Updated casualty figures from MOD\"}" \
  "{\"user\":\"UkraineDefends\",\"timestamp\":$((NOW-4800)),\"byte_change\":-2800,\"comment\":\"Removed Russian propaganda numbers\"}" \
  "{\"user\":\"Kremlin_watcher\",\"timestamp\":$((NOW-4200)),\"byte_change\":2200,\"comment\":\"Re-added with TASS source\"}" \
  "{\"user\":\"NATOanalyst\",\"timestamp\":$((NOW-3600)),\"byte_change\":800,\"comment\":\"Added Western intelligence estimates\"}" \
  "{\"user\":\"UkraineDefends\",\"timestamp\":$((NOW-3000)),\"byte_change\":-2000,\"comment\":\"TASS is not reliable per consensus\"}" \
  "{\"user\":\"Kremlin_watcher\",\"timestamp\":$((NOW-2400)),\"byte_change\":1500,\"comment\":\"Restored with multiple sources\"}" \
  "{\"user\":\"WarCorrespondent\",\"timestamp\":$((NOW-1800)),\"byte_change\":500,\"comment\":\"Added on-ground report\"}" \
  "{\"user\":\"UkraineDefends\",\"timestamp\":$((NOW-1200)),\"byte_change\":-1200,\"comment\":\"Reverted unverified claims\"}" \
  "{\"user\":\"Kremlin_watcher\",\"timestamp\":$((NOW-600)),\"byte_change\":1800,\"comment\":\"Fourth revert with ISW data\"}" \
  "{\"user\":\"NATOanalyst\",\"timestamp\":$((NOW-120)),\"byte_change\":400,\"comment\":\"Balanced both sides\"}" > /dev/null

$REDIS DEL "editwar:editors:$PAGE" > /dev/null 2>&1 || true
$REDIS HSET "editwar:editors:$PAGE" \
  "Kremlin_watcher" "4" \
  "UkraineDefends" "3" \
  "NATOanalyst" "2" \
  "WarCorrespondent" "1" > /dev/null

$REDIS DEL "editwar:changes:$PAGE" > /dev/null 2>&1 || true
$REDIS RPUSH "editwar:changes:$PAGE" "3000" "-2800" "2200" "800" "-2000" "1500" "500" "-1200" "1800" "400" > /dev/null

# Marker key
$REDIS SET "editwar:$PAGE" "1" EX 43200 > /dev/null

$REDIS SET "editwar:start:$PAGE" "$(date -d @$((NOW-5400)) -u +%Y-%m-%dT%H:%M:%SZ)" EX 43200 > /dev/null
$REDIS SET "editwar:serverurl:$PAGE" "https://en.wikipedia.org" EX 43200 > /dev/null

$REDIS EXPIRE "editwar:timeline:$PAGE" 43200 > /dev/null
$REDIS EXPIRE "editwar:editors:$PAGE" 43200 > /dev/null
$REDIS EXPIRE "editwar:changes:$PAGE" 43200 > /dev/null

echo "✓ Injected: $PAGE (10 edits, 4 editors)"

###############################################################################
# 3. KASHMIR CONFLICT
###############################################################################
PAGE="Kashmir conflict"

# Timeline already injected above; add other keys
$REDIS DEL "editwar:editors:$PAGE" > /dev/null 2>&1 || true
$REDIS HSET "editwar:editors:$PAGE" \
  "IndiaFirst92" "3" \
  "PakDefender" "3" \
  "NeutralEditor7" "1" \
  "KashmirWatch" "1" > /dev/null

$REDIS DEL "editwar:changes:$PAGE" > /dev/null 2>&1 || true
$REDIS RPUSH "editwar:changes:$PAGE" "1200" "-1100" "950" "300" "-800" "1100" "400" "-600" > /dev/null

# Marker key
$REDIS SET "editwar:$PAGE" "1" EX 43200 > /dev/null

$REDIS SET "editwar:start:$PAGE" "$(date -d @$((NOW-3600)) -u +%Y-%m-%dT%H:%M:%SZ)" EX 43200 > /dev/null
$REDIS SET "editwar:serverurl:$PAGE" "https://en.wikipedia.org" EX 43200 > /dev/null

$REDIS EXPIRE "editwar:timeline:$PAGE" 43200 > /dev/null
$REDIS EXPIRE "editwar:editors:$PAGE" 43200 > /dev/null
$REDIS EXPIRE "editwar:changes:$PAGE" 43200 > /dev/null

echo "✓ Injected: $PAGE (8 edits, 4 editors)"

###############################################################################
# Verify
###############################################################################
echo ""
echo "=== Verification ==="
echo -n "Marker keys: "
$REDIS KEYS "editwar:*" 2>/dev/null | while read key; do
  rest="${key#editwar:}"
  if [[ "$rest" != *":"* ]]; then
    echo -n "[$rest] "
  fi
done
echo ""
echo -n "US election timeline entries: "
$REDIS LLEN "editwar:timeline:2024 United States presidential election"
echo -n "Russia-Ukraine timeline entries: "
$REDIS LLEN "editwar:timeline:Russian invasion of Ukraine"
echo -n "Kashmir timeline entries: "
$REDIS LLEN "editwar:timeline:Kashmir conflict"
