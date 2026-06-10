#!/usr/bin/env bash
set -u

CHAPS_URL="http://localhost:8420"
FPS_URL="http://localhost:8421"
BACS_URL="http://localhost:8422"
RESET_DOCKER_VOLUMES=0
PASSED=0
FAILED=0
REGISTERED_API_KEY=""

while [ $# -gt 0 ]; do
  case "$1" in
    --chaps-url) CHAPS_URL="$2"; shift 2 ;;
    --fps-url) FPS_URL="$2"; shift 2 ;;
    --bacs-url) BACS_URL="$2"; shift 2 ;;
    --reset-docker-volumes) RESET_DOCKER_VOLUMES=1; shift ;;
    -h|--help)
      cat <<EOF
Usage: ./test/bank_workflows.sh [options]

Options:
  --chaps-url URL              Default: http://localhost:8420
  --fps-url URL                Default: http://localhost:8421
  --bacs-url URL               Default: http://localhost:8422
  --reset-docker-volumes       Run docker compose down -v && docker compose up -d --build first
EOF
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

pass() {
  PASSED=$((PASSED + 1))
  printf '  PASS %s\n' "$1"
}

fail() {
  FAILED=$((FAILED + 1))
  printf '  FAIL %s\n' "$1"
}

new_id() {
  printf '%s%03d' "$(date +%s%3N 2>/dev/null || date +%s)" "$((RANDOM % 900 + 100))"
}

new_bic() {
  printf '%s%06d' "$1" "$((RANDOM % 900000 + 100000))"
}

new_sort_code() {
  printf '%02d-%02d-%02d' "$((RANDOM % 90 + 10))" "$((RANDOM % 90 + 10))" "$((RANDOM % 90 + 10))"
}

json_escape() {
  python3 -c 'import json,sys; print(json.dumps(sys.argv[1])[1:-1])' "$1"
}

extract_api_key() {
  python3 -c 'import json,sys; print(json.loads(sys.stdin.read()).get("api_key",""))' 2>/dev/null
}

request() {
  local method="$1" url="$2" token="${3:-}" content_type="${4:-application/json}" body="${5:-}"
  local header_args=(-H "Content-Type: $content_type")
  if [ -n "$token" ]; then
    header_args+=(-H "Authorization: Bearer $token")
  fi

  if [ -n "$body" ]; then
    curl -sS -X "$method" "${header_args[@]}" -d "$body" -w '\n%{http_code}' "$url"
  else
    curl -sS -X "$method" "${header_args[@]}" -w '\n%{http_code}' "$url"
  fi
}

status_of() {
  printf '%s' "$1" | tail -n 1
}

body_of() {
  printf '%s' "$1" | sed '$d'
}

assert_status() {
  local name="$1" response="$2"
  shift 2
  local status
  status="$(status_of "$response")"
  for expected in "$@"; do
    if [ "$status" = "$expected" ]; then
      pass "$name -> HTTP $status"
      return 0
    fi
  done
  local body
  body="$(body_of "$response" | head -c 240)"
  fail "$name -> HTTP $status, expected $*. Body: $body"
  return 1
}

register_chaps_bank() {
  local base="$1" bic="$2" name="$3" sort_code="$4" balance="$5"
  local body response
  REGISTERED_API_KEY=""
  body=$(printf '{"bic":"%s","name":"%s","sort_code":"%s","balance":%s}' \
    "$bic" "$(json_escape "$name")" "$sort_code" "$balance")
  response="$(request POST "$base/v1/participants/register" "" "application/json" "$body")"
  assert_status "CHAPS register $bic" "$response" 201
  REGISTERED_API_KEY="$(body_of "$response" | extract_api_key)"
}

register_fps_bank() {
  local base="$1" bic="$2" name="$3" sort_code="$4" balance="$5"
  local body response
  REGISTERED_API_KEY=""
  body=$(printf '{"bic":"%s","name":"%s","sort_code":"%s","balance":%s,"participant_type":"DIRECT"}' \
    "$bic" "$(json_escape "$name")" "$sort_code" "$balance")
  response="$(request POST "$base/v1/participants/register" "" "application/json" "$body")"
  assert_status "FPS register $bic" "$response" 201
  REGISTERED_API_KEY="$(body_of "$response" | extract_api_key)"
}

register_bacs_bank() {
  local base="$1" bic="$2" name="$3" sort_code="$4" su_code="$5" balance="$6"
  local body response
  REGISTERED_API_KEY=""
  body=$(printf '{"bic":"%s","name":"%s","sort_code":"%s","su_code":"%s","balance":%s,"is_service_user":true,"is_destination_user":true}' \
    "$bic" "$(json_escape "$name")" "$sort_code" "$su_code" "$balance")
  response="$(request POST "$base/v1/participants/register" "" "application/json" "$body")"
  assert_status "BACS register $bic" "$response" 201
  REGISTERED_API_KEY="$(body_of "$response" | extract_api_key)"
}

std18_line() {
  local value="$1"
  printf '%-80.80s' "$value"
}

standard18_file() {
  local dest_sort="$1" dest_account="$2" originator="$3" su_code="$4" amount_pence="$5" ref="$6"
  local amount
  amount="$(printf '%011d' "$amount_pence")"
  std18_line "10000001${dest_sort}$(printf '%-9s' "$dest_account")                             ${amount}0000001260610"
  printf '\n'
  std18_line "40000001${dest_sort}$(printf '%-9s' "$dest_account")${amount}$(printf '%-15s' "$originator")$(printf '%-14s' "$ref")$(printf '%-13s' "$su_code")"
  printf '\n'
  std18_line "50000001                                        00000001"
  printf '\n'
  std18_line "90000001            ${amount}00000000100000000000001"
}

require_api_key() {
  local service="$1" token="$2"
  if [ -z "$token" ]; then
    fail "$service workflow blocked: registration did not return api_key"
    return 1
  fi
  return 0
}

test_chaps_workflow() {
  printf '\n=== CHAPS bank workflow ===\n'
  assert_status "CHAPS health" "$(request GET "$CHAPS_URL/v1/healthz")" 200

  local id bank1 bank2 sort1 sort2 token1 token2 response body
  id="$(new_id)"
  bank1="$(new_bic CA)"
  bank2="$(new_bic CB)"
  sort1="$(new_sort_code)"
  sort2="$(new_sort_code)"

  register_chaps_bank "$CHAPS_URL" "$bank1" "Integration CHAPS Bank $id A" "$sort1" 1000000
  token1="$REGISTERED_API_KEY"
  require_api_key "CHAPS first bank" "$token1" || return

  body=$(printf '{"msg_id":"CHAPS-SEED-%s","end_to_end_id":"E2E-SEED-%s","receiver_bic":"HSBCGB44","receiver_sort_code":"40-00-00","amount":1000.00}' "$id" "$id")
  assert_status "CHAPS payment to seeded HSBC" "$(request POST "$CHAPS_URL/v1/payments/chaps" "$token1" "application/json" "$body")" 200 202

  register_chaps_bank "$CHAPS_URL" "$bank2" "Integration CHAPS Bank $id B" "$sort2" 500000
  token2="$REGISTERED_API_KEY"
  require_api_key "CHAPS second bank" "$token2" || return

  body=$(printf '{"msg_id":"CHAPS-BANK-%s","end_to_end_id":"E2E-BANK-%s","receiver_bic":"%s","receiver_sort_code":"%s","amount":125.50}' "$id" "$id" "$bank2" "$sort2")
  assert_status "CHAPS payment to newly registered bank account" "$(request POST "$CHAPS_URL/v1/payments/chaps" "$token1" "application/json" "$body")" 200 202

  body=$(printf '{"msg_id":"CHAPS-NOAUTH-%s","receiver_bic":"HSBCGB44","receiver_sort_code":"40-00-00","amount":10}' "$id")
  assert_status "CHAPS rejects missing auth" "$(request POST "$CHAPS_URL/v1/payments/chaps" "" "application/json" "$body")" 401

  body=$(printf '{"msg_id":"CHAPS-BADAMT-%s","receiver_bic":"HSBCGB44","receiver_sort_code":"40-00-00","amount":-1}' "$id")
  assert_status "CHAPS rejects negative amount" "$(request POST "$CHAPS_URL/v1/payments/chaps" "$token1" "application/json" "$body")" 400

  body=$(printf '{"msg_id":"CHAPS-NOSORT-%s","receiver_bic":"HSBCGB44","amount":10}' "$id")
  assert_status "CHAPS rejects missing receiver sort code" "$(request POST "$CHAPS_URL/v1/payments/chaps" "$token1" "application/json" "$body")" 400

  assert_status "CHAPS rejects invalid registration values" \
    "$(request POST "$CHAPS_URL/v1/participants/register" "" "application/json" '{"bic":"bad","name":"Bad CHAPS Bank","sort_code":"","balance":-10}')" 400

  : "$token2"
}

test_fps_workflow() {
  printf '\n=== FPS bank workflow ===\n'
  assert_status "FPS health" "$(request GET "$FPS_URL/v1/healthz")" 200

  local id bank1 bank2 sort1 sort2 token1 token2 body
  id="$(new_id)"
  bank1="$(new_bic FA)"
  bank2="$(new_bic FB)"
  sort1="$(new_sort_code)"
  sort2="$(new_sort_code)"

  register_fps_bank "$FPS_URL" "$bank1" "Integration FPS Bank $id A" "$sort1" 500000
  token1="$REGISTERED_API_KEY"
  require_api_key "FPS first bank" "$token1" || return

  body=$(printf '{"msg_id":"FPS-SEED-%s","end_to_end_id":"E2E-SEED-%s","receiver_bic":"HSBCGB44","receiver_sort_code":"40-00-00","amount":25.00}' "$id" "$id")
  assert_status "FPS payment to seeded HSBC" "$(request POST "$FPS_URL/v1/payments/fps" "$token1" "application/json" "$body")" 200 202

  register_fps_bank "$FPS_URL" "$bank2" "Integration FPS Bank $id B" "$sort2" 250000
  token2="$REGISTERED_API_KEY"
  require_api_key "FPS second bank" "$token2" || return

  body=$(printf '{"msg_id":"FPS-BANK-%s","end_to_end_id":"E2E-BANK-%s","receiver_bic":"%s","receiver_sort_code":"%s","amount":17.45}' "$id" "$id" "$bank2" "$sort2")
  assert_status "FPS payment to newly registered bank account" "$(request POST "$FPS_URL/v1/payments/fps" "$token1" "application/json" "$body")" 200 202

  body=$(printf '{"msg_id":"FPS-NOAUTH-%s","receiver_bic":"HSBCGB44","receiver_sort_code":"40-00-00","amount":10}' "$id")
  assert_status "FPS rejects missing auth" "$(request POST "$FPS_URL/v1/payments/fps" "" "application/json" "$body")" 401

  body=$(printf '{"msg_id":"FPS-BADAMT-%s","receiver_bic":"HSBCGB44","receiver_sort_code":"40-00-00","amount":-1}' "$id")
  assert_status "FPS rejects negative amount" "$(request POST "$FPS_URL/v1/payments/fps" "$token1" "application/json" "$body")" 400

  body=$(printf '{"bic":"%s","name":"Bad FPS Bank %s","sort_code":"%s","balance":100,"participant_type":"UNKNOWN"}' "$(new_bic FC)" "$id" "$(new_sort_code)")
  assert_status "FPS rejects invalid participant type" "$(request POST "$FPS_URL/v1/participants/register" "" "application/json" "$body")" 400

  : "$token2"
}

test_bacs_workflow() {
  printf '\n=== BACS bank workflow ===\n'
  assert_status "BACS health" "$(request GET "$BACS_URL/v1/healthz")" 200

  local id bank1 bank2 sort1 sort2 token1 token2 su1 su2 file body
  id="$(new_id)"
  bank1="$(new_bic BA)"
  bank2="$(new_bic BB)"
  sort1="$(new_sort_code)"
  sort2="$(new_sort_code)"
  su1="SU${id: -8}"
  su2="SB${id: -8}"

  register_bacs_bank "$BACS_URL" "$bank1" "Integration BACS Bank $id A" "$sort1" "$su1" 500000
  token1="$REGISTERED_API_KEY"
  require_api_key "BACS first bank" "$token1" || return

  file="$(standard18_file "40-00-00" "000123456" "BACS TEST A" "$su1" 12345 "SEED$id")"
  assert_status "BACS Standard 18 submission to seeded HSBC sort code" \
    "$(request POST "$BACS_URL/v1/payments/bacs/submit?filename=seed-$id.txt" "$token1" "text/plain" "$file")" 201 202

  register_bacs_bank "$BACS_URL" "$bank2" "Integration BACS Bank $id B" "$sort2" "$su2" 250000
  token2="$REGISTERED_API_KEY"
  require_api_key "BACS second bank" "$token2" || return

  file="$(standard18_file "$sort2" "000654321" "BACS TEST B" "$su1" 6789 "BANK$id")"
  assert_status "BACS Standard 18 submission to newly registered bank account" \
    "$(request POST "$BACS_URL/v1/payments/bacs/submit?filename=bank-$id.txt" "$token1" "text/plain" "$file")" 201 202

  assert_status "BACS rejects missing auth" \
    "$(request POST "$BACS_URL/v1/payments/bacs/submit?filename=noauth-$id.txt" "" "text/plain" "$file")" 401

  assert_status "BACS rejects malformed Standard 18" \
    "$(request POST "$BACS_URL/v1/payments/bacs/submit?filename=bad-$id.txt" "$token1" "text/plain" "not-a-standard-18-file")" 400

  assert_status "BACS rejects invalid registration values" \
    "$(request POST "$BACS_URL/v1/participants/register" "" "application/json" '{"bic":"bad","name":"Bad BACS Bank","sort_code":"","balance":-10}')" 400

  : "$token2"
}

reset_compose_if_requested() {
  if [ "$RESET_DOCKER_VOLUMES" -ne 1 ]; then
    return
  fi
  local root
  root="$(cd "$(dirname "$0")/.." && pwd)"
  echo "Resetting Docker Compose volumes and rebuilding services..."
  (cd "$root" && docker compose down -v && docker compose up -d --build)
}

echo "UKPS bank integration workflow tests"
echo "CHAPS: $CHAPS_URL"
echo "FPS:   $FPS_URL"
echo "BACS:  $BACS_URL"

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "python3 is required for JSON parsing" >&2; exit 2; }

reset_compose_if_requested

test_chaps_workflow
test_fps_workflow
test_bacs_workflow

printf '\nResults: %d passed, %d failed\n' "$PASSED" "$FAILED"
[ "$FAILED" -eq 0 ]
