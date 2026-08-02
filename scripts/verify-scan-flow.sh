#!/usr/bin/env bash
# verify-scan-flow.sh — end-to-end QR generation check against a deployed
# instance. Proves the backend actually produces a scannable, rotating QR for
# a session (the worker-start fix) and prints the QR's decoded payload so it
# can be compared with the external Warwick site's QR for the same session.
#
# Usage:
#   scripts/verify-scan-flow.sh [session_id] [base_url]
#   scripts/verify-scan-flow.sh [session_id] [base_url] --rotate   # proves the QR rotates
#   scripts/verify-scan-flow.sh [session_id] [base_url] --timing   # measures token TTL vs refresh cadence
#
# Defaults: session_id=18898, base_url=https://classroom-warwick.up.railway.app
#
# Requires: curl, python3 (JSON parsing), and the Go decoder in scripts/qrdecode
# (first run does `go mod tidy` inside it).

set -u

SESSION="${1:-18898}"
BASE="${2:-https://classroom-warwick.up.railway.app}"
ROTATE="${3:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

json_field() { python3 -c "import json,sys; d=json.load(sys.stdin); print(d$1)" 2>/dev/null; }

echo "==> Starting QR room for session $SESSION on $BASE (wakes the server if asleep)..."
RESP=$(curl -s --max-time 90 -X POST "$BASE/api/rooms/from-session/start" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SESSION\"}") || fail "POST /from-session/start failed (network?)"
echo "    response: $(echo "$RESP" | head -c 160)"

echo "==> Polling room detail for qr_url (up to 40s)..."
QR_URL=""
STATUS=""
for _ in $(seq 1 40); do
  DETAIL=$(curl -s --max-time 30 "$BASE/api/rooms/$SESSION")
  STATUS=$(echo "$DETAIL" | json_field "['data']['status']")
  QR_URL=$(echo "$DETAIL" | json_field "['data']['qr_url']")
  WARN=$(echo "$DETAIL" | json_field "['data']['warning_message']" || true)
  ERR=$(echo "$DETAIL" | json_field "['data']['error_message']" || true)
  if [ -n "$QR_URL" ] && [ "$QR_URL" != "None" ]; then
    break
  fi
  if [ -n "$ERR" ] && [ "$ERR" != "None" ]; then
    fail "room $SESSION is $STATUS with error: $ERR"
  fi
  sleep 1
done

if [ -z "$QR_URL" ] || [ "$QR_URL" = "None" ]; then
  echo "    room status: ${STATUS:-unknown}, warning: ${WARN:-none}"
  fail "no qr_url after 40s — is the backend worker fix deployed? (old bug: persisted Running room never starts a worker)"
fi
pass "QR generated for session $SESSION (room status: $STATUS)"

echo "==> Decoding QR payload (same content a student's phone sees)..."
PAYLOAD=$(cd "$SCRIPT_DIR/qrdecode" && go run . "$QR_URL") || fail "QR decode failed"
echo "    decoded: $PAYLOAD"
pass "QR is a valid QR code"

case "$PAYLOAD" in
  *liff.line.me*)   pass "payload is a LINE LIFF check-in link (Warwick's student Mini App) — the same flow the external site's QR uses" ;;
  *humantix.cloud*) pass "payload references warwick.humantix.cloud" ;;
  *)                fail "payload is not a recognizable check-in link: $PAYLOAD" ;;
esac

# Save the QR image so it can be compared side by side with the external
# site's QR for the same session.
PNG_FILE="/tmp/qr_session_${SESSION}.png"
echo "$QR_URL" | python3 -c "
import base64, sys
uri = sys.stdin.read().strip()
b64 = uri.split(',', 1)[1].strip()
open('$PNG_FILE', 'wb').write(base64.b64decode(b64))
" 2>/dev/null && echo "    QR image saved to $PNG_FILE"
echo "    NOTE: the payload token rotates on every refresh and is opaque — it will NOT contain the session id."
echo "    Compare the URL SHAPE (e.g. liff.line.me/...?token=<uuid>) with the external site's QR,"
echo "    and scan both with a phone: both must open the same check-in app."

if [ "$ROTATE" = "--rotate" ]; then
  echo "==> Waiting 65s to confirm the QR rotates (fresh code for students)..."
  FIRST="$QR_URL"
  sleep 65
  DETAIL=$(curl -s --max-time 30 "$BASE/api/rooms/$SESSION")
  SECOND=$(echo "$DETAIL" | json_field "['data']['qr_url']")
  if [ -n "$SECOND" ] && [ "$SECOND" != "None" ] && [ "$SECOND" != "$FIRST" ]; then
    pass "QR rotated to a new value (stale-code problem fixed)"
  else
    fail "QR did not rotate after 65s (still showing the old code)"
  fi
fi

if [ "$ROTATE" = "--timing" ]; then
  echo "==> Measuring token TTL and actual refresh cadence (up to 90s)..."
  python3 - "$BASE" "$SESSION" <<'PY'
import json, sys, time, urllib.request

base, session = sys.argv[1], sys.argv[2]

def room():
    with urllib.request.urlopen(f"{base}/api/rooms/{session}", timeout=30) as r:
        return json.load(r)["data"]

def parse(ts):
    return time.mktime(time.strptime(ts[:19], "%Y-%m-%dT%H:%M:%S")) + float("." + (ts.split(".")[1][:6] if "." in ts else "0"))

d = room()
fetched, expires = d.get("last_fetch_at"), d.get("expires_at")
if not fetched or not expires:
    print("FAIL: room has no last_fetch_at/expires_at"); sys.exit(1)
ttl = parse(expires) - parse(fetched)
print(f"    token TTL (expires_at - last_fetch_at) = {ttl:.0f}s")

start = time.time()
last_fetch = fetched
rotations = []
while time.time() - start < 90:
    d = room()
    if d.get("last_fetch_at") and d["last_fetch_at"] != last_fetch:
        interval = parse(d["last_fetch_at"]) - parse(last_fetch)
        rotations.append(interval)
        print(f"    rotation at +{time.time()-start:.0f}s: refresh interval = {interval:.1f}s")
        last_fetch = d["last_fetch_at"]
        if len(rotations) >= 2:
            break
    time.sleep(2)

if not rotations:
    print("FAIL: no QR refresh observed within 90s (worker not refreshing?)")
    sys.exit(1)

interval = sum(rotations) / len(rotations)
expected = ttl * 0.75
print(f"    observed refresh interval = {interval:.1f}s, expected 75% of TTL = {expected:.1f}s")
lo, hi = ttl * 0.5, ttl
if lo <= interval <= hi:
    print(f"PASS: refresh interval ({interval:.1f}s) is near the documented 75%-of-TTL cadence "
          f"and within the token lifetime ({ttl:.0f}s) — the room always holds a valid token")
elif interval < lo:
    print(f"WARN: refresh interval ({interval:.1f}s) is only {interval/ttl:.0%} of the TTL — "
          f"too aggressive (old shouldFetchQR treated 75% as the expiry margin, "
          f"making the period 25% of TTL). Redeploy the worker fix.")
else:
    print(f"FAIL: refresh interval ({interval:.1f}s) exceeds the token TTL ({ttl:.0f}s) — "
          f"stale-token risk")
    sys.exit(1)
PY
fi

echo
echo "NEXT STEP (definitive, requires a phone):"
echo "  1. Open the QR dialog in the app for session $SESSION."
echo "  2. Scan it with a student's phone (LINE app installed and linked to their Warwick account)."
echo "  3. Complete the check-in that opens (Warwick's LINE Mini App)."
echo "  4. Verify 'Checked in' appears on the Warwick admin ClassAttendance page"
echo "     and in the app's roster (refresh the check-in page if needed)."
