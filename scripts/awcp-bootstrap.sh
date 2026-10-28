#!/bin/sh
# Fetch Postgres/Redis/event/signing-key values from the AWCP sidecar and
# assemble INNGEST_POSTGRES_URI / INNGEST_REDIS_URI before exec'ing the real
# server. Values never pass through a persistent k8s Secret; AWCP serves them
# fresh over loopback on each pod start (see apps/inngest in privy-io/kubernetes).
set -eu

: "${AWS_REGION:?AWS_REGION must be set}"
: "${AWS_ACCOUNT_ID:?AWS_ACCOUNT_ID must be set}"
: "${AWCP_READER_ROLE_ARN:?AWCP_READER_ROLE_ARN must be set}"
: "${AWCP_SECRET_PREFIX:?AWCP_SECRET_PREFIX must be set}" # e.g. stage/inngest/
: "${INNGEST_POSTGRES_HOST:?INNGEST_POSTGRES_HOST must be set}"

awcp_endpoint="${AWCP_ENDPOINT:-http://127.0.0.1:2773}"
awcp_token_file="${AWCP_TOKEN_FILE:-/var/run/awcp/token}"
postgres_port="${INNGEST_POSTGRES_PORT:-5432}"
postgres_db="${INNGEST_POSTGRES_DB:-inngest}"
redis_port="${INNGEST_REDIS_PORT:-6379}"

token="$(cat "$awcp_token_file")"

urlencode() {
  jq -rn --arg v "$1" '$v | @uri'
}

fetch_secret() {
  name="$1"
  secret_id="arn:aws:secretsmanager:${AWS_REGION}:${AWS_ACCOUNT_ID}:secret:${AWCP_SECRET_PREFIX}${name}"
  curl -fsS --max-time 10 \
    -H "X-Aws-Parameters-Secrets-Token: ${token}" \
    --get \
    --data-urlencode "secretId=${secret_id}" \
    --data-urlencode "roleArn=${AWCP_READER_ROLE_ARN}" \
    "${awcp_endpoint}/secretsmanager/get" \
    | jq -er '.SecretString' || {
      echo "awcp-bootstrap: failed to read ${name}" >&2
      exit 1
    }
}

postgres_username="$(fetch_secret postgres-username)"
postgres_password="$(fetch_secret postgres-password)"
redis_password="$(fetch_secret redis-password)"
redis_host="$(fetch_secret redis-host)"
event_key="$(fetch_secret event-key)"
signing_key="$(fetch_secret signing-key)"

postgres_uri_user="$(urlencode "$postgres_username")"
postgres_uri_pass="$(urlencode "$postgres_password")"
redis_uri_pass="$(urlencode "$redis_password")"

export INNGEST_EVENT_KEY="$event_key"
export INNGEST_SIGNING_KEY="$signing_key"
export INNGEST_POSTGRES_URI="postgres://${postgres_uri_user}:${postgres_uri_pass}@${INNGEST_POSTGRES_HOST}:${postgres_port}/${postgres_db}?sslmode=require"
export INNGEST_REDIS_URI="rediss://:${redis_uri_pass}@${redis_host}:${redis_port}"

echo "awcp-bootstrap: loaded 6 secrets, starting inngest" >&2

exec /bin/inngest "$@"
