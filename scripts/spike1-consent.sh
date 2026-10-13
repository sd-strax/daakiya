#!/usr/bin/env bash
# Spike 1: can this account reach its own mailbox through Microsoft Graph with the
# permissions Daakiya needs? Throwaway script, no dependencies beyond curl and python3.
#
# Usage: scripts/spike1-consent.sh <client_id> <tenant_id> [--yes]
#   --yes  skip the confirmation before the move test (needed when run without a tty)
#
# Everything it does to the mailbox is reversible and it asks before touching a message.
set -u

CLIENT_ID="${1:?usage: $0 <client_id> <tenant_id>}"
TENANT="${2:?usage: $0 <client_id> <tenant_id>}"
YES="${3:-}"
AUTH="https://login.microsoftonline.com/$TENANT/oauth2/v2.0"
GRAPH="https://graph.microsoft.com/v1.0"
SCOPES="https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/MailboxSettings.ReadWrite https://graph.microsoft.com/User.Read offline_access"

jget() { python3 -c 'import sys,json
d=json.load(sys.stdin)
for k in sys.argv[1].split("/"):
    d=d[int(k)] if k.isdigit() else d.get(k, "")
    if d=="" : break
print(d if not isinstance(d,(dict,list)) else json.dumps(d))' "$1"; }

say()  { printf '\n== %s\n' "$*"; }
fail() { printf '\nFAIL: %s\n' "$*"; exit 1; }

say "Step 6: device code sign-in"
DC=$(curl -s -X POST "$AUTH/devicecode" --data-urlencode "client_id=$CLIENT_ID" --data-urlencode "scope=$SCOPES")
if [ -n "$(printf %s "$DC" | jget error)" ]; then
  printf '%s\n' "$DC" | jget error_description
  fail "device code request rejected. If it mentions AADSTS7000218 or public client, step 4 (Allow public client flows) is not saved."
fi
printf '%s\n\n' "$(printf %s "$DC" | jget message)"
DEVICE_CODE=$(printf %s "$DC" | jget device_code)
INTERVAL=$(printf %s "$DC" | jget interval); INTERVAL=${INTERVAL:-5}

TOKEN=""
while :; do
  sleep "$INTERVAL"
  T=$(curl -s -X POST "$AUTH/token" \
       --data-urlencode "grant_type=urn:ietf:params:oauth:grant-type:device_code" \
       --data-urlencode "client_id=$CLIENT_ID" \
       --data-urlencode "device_code=$DEVICE_CODE")
  ERR=$(printf %s "$T" | jget error)
  case "$ERR" in
    "") TOKEN=$(printf %s "$T" | jget access_token); break ;;
    authorization_pending) printf '.'; ;;
    *) printf '\n%s\n' "$(printf %s "$T" | jget error_description)"
       case "$(printf %s "$T" | jget error_description)" in
         *AADSTS65001*|*AADSTS65004*|*AADSTS90094*|*AADSTS900941*)
           fail "CONSENT BLOCKED. The tenant does not let you grant Mail.ReadWrite yourself. Outcome for spike 1: 'consent blocked'. Ask IT for admin consent on app $CLIENT_ID (Entra > App registrations > Daakiya > API permissions > Grant admin consent), or check whether the 'Request approval' button appeared in the browser (admin consent workflow)." ;;
         *) fail "sign-in failed: $ERR" ;;
       esac ;;
  esac
done
[ -n "$TOKEN" ] || fail "no access token"
printf '\nSigned in and consent granted. Refresh token present: %s\n' "$( [ -n "$(printf %s "$T" | jget refresh_token)" ] && echo yes || echo NO )"

g() { # method path [json-body]
  if [ $# -ge 3 ]; then
    curl -s -X "$1" "$GRAPH$2" -H "Authorization: Bearer $TOKEN" -H 'Prefer: IdType="ImmutableId"' \
         -H "Content-Type: application/json" --data "$3"
  else
    curl -s -X "$1" "$GRAPH$2" -H "Authorization: Bearer $TOKEN" -H 'Prefer: IdType="ImmutableId"'
  fi
}
check() { # label json
  E=$(printf %s "$2" | jget error/message)
  if [ -n "$E" ]; then printf '  %-32s FAIL  %s\n' "$1" "$E"; return 1; fi
  printf '  %-32s ok\n' "$1"; return 0
}

say "Step 7: Graph checks"
ME=$(g GET "/me?\$select=displayName,mail,userPrincipalName"); check "who am I" "$ME" && printf '  -> %s <%s>\n' "$(printf %s "$ME" | jget displayName)" "$(printf %s "$ME" | jget mail)"

DELTA=$(g GET "/me/mailFolders/inbox/messages/delta?\$select=subject,from,receivedDateTime&\$top=5")
check "inbox delta (immutable ids)" "$DELTA" || fail "cannot read inbox"
LINK=$(printf %s "$DELTA" | jget '@odata.deltaLink'); [ -n "$LINK" ] || LINK=$(printf %s "$DELTA" | jget '@odata.nextLink')
printf '  -> delta/next link present: %s\n' "$( [ -n "$LINK" ] && echo yes || echo NO )"
MSG_ID=$(printf %s "$DELTA" | jget value/0/id)
MSG_SUBJ=$(printf %s "$DELTA" | jget value/0/subject)

CAT=$(g GET "/me/outlook/masterCategories")
check "read master categories" "$CAT"
NEWCAT=$(g POST "/me/outlook/masterCategories" '{"displayName":"Daakiya spike","color":"preset0"}')
if check "create master category" "$NEWCAT"; then
  CAT_ID=$(printf %s "$NEWCAT" | jget id)
  R=$(g DELETE "/me/outlook/masterCategories/$CAT_ID"); check "delete master category" "${R:-"{}"}"
fi

FOLDER=$(g POST "/me/mailFolders" '{"displayName":"Daakiya spike"}')
check "create folder" "$FOLDER" || fail "cannot create folders"
FOLDER_ID=$(printf %s "$FOLDER" | jget id)

if [ -n "$MSG_ID" ]; then
  printf '\n  The move test will move this message to the test folder and straight back:\n    "%s"\n  Proceed? [y/N] ' "$MSG_SUBJ"
  if [ "$YES" = "--yes" ]; then yn=y; printf 'y (--yes)\n'; else read -r yn; fi
  if [ "$yn" = "y" ] || [ "$yn" = "Y" ]; then
    MOVED=$(g POST "/me/messages/$MSG_ID/move" "{\"destinationId\":\"$FOLDER_ID\"}")
    if check "move message out" "$MOVED"; then
      NEW_ID=$(printf %s "$MOVED" | jget id)
      printf '  -> id unchanged after move (immutable ids working): %s\n' "$( [ "$NEW_ID" = "$MSG_ID" ] && echo yes || echo NO )"
      BACK=$(g POST "/me/messages/$NEW_ID/move" '{"destinationId":"inbox"}'); check "move message back" "$BACK"
      BACK_ID=$(printf %s "$BACK" | jget id); [ -n "$BACK_ID" ] || BACK_ID=$NEW_ID
      P=$(g PATCH "/me/messages/$BACK_ID" '{"categories":["Daakiya spike"]}'); check "set category on message" "$P"
      P=$(g PATCH "/me/messages/$BACK_ID" '{"categories":[]}'); check "clear category on message" "$P"
    fi
  else
    printf '  move/category test skipped\n'
  fi
else
  printf '  inbox returned no messages; move test skipped\n'
fi

R=$(g DELETE "/me/mailFolders/$FOLDER_ID"); check "delete test folder" "${R:-"{}"}"

say "Done. Outcome for spike 1: PROCEED if every line above says ok."
