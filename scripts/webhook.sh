#!/usr/bin/env bash
set -euo pipefail
set -a
source "${ENV_FILE:-.env}"
set +a
: "${TELEGRAM_BOT_TOKEN:?Set TELEGRAM_BOT_TOKEN in .env}"
action="${1:-getWebhookInfo}"
args=()
case "$action" in
  setWebhook)
    : "${WEBHOOK_URL:?Set WEBHOOK_URL=https://host/webhook}"
    : "${TELEGRAM_WEBHOOK_SECRET:?Set TELEGRAM_WEBHOOK_SECRET in .env}"
    [[ "$WEBHOOK_URL" == https://* ]] || { echo 'WEBHOOK_URL must use HTTPS' >&2; exit 1; }
    args+=(--data-urlencode "url=$WEBHOOK_URL"
      --data-urlencode "secret_token=$TELEGRAM_WEBHOOK_SECRET"
      --data-urlencode 'allowed_updates=["message"]')
    ;;
  getWebhookInfo|deleteWebhook) ;;
  *) echo 'Unknown webhook action' >&2; exit 1 ;;
esac
curl --fail --silent --show-error --connect-timeout 10 --max-time 30 --request POST \
  "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/$action" "${args[@]}" |
  python3 -c 'import json,sys; r=json.load(sys.stdin); print(json.dumps(r,ensure_ascii=False,indent=2)); sys.exit(0 if r.get("ok") else 1)'
