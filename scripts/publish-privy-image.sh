#!/usr/bin/env bash
# Build and push the self-hosted Inngest image (with the AWCP bootstrap
# wrapper, see scripts/awcp-bootstrap.sh) to Privy's ECR repo. Manual process
# for now — no CI publish pipeline exists for this fork yet.
#
# Usage: scripts/publish-privy-image.sh --environment stage|prod
#
# AWS credentials: set AWS_PROFILE to the matching SSO profile before running:
#   stage -> AWS_PROFILE=stage-blob-ops  (account 904923506875)
#   prod  -> AWS_PROFILE=prod-blob-ops   (account 798406098447)
# Run `aws sso login --profile <profile>` first if the session has expired.
#
# Refuses to run anywhere except a clean checkout of origin/main, so a
# pushed image always traces back to a reviewed, merged commit.
set -euo pipefail

region="us-east-1"
environment=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --environment) environment="${2:-}"; shift 2 ;;
    -h|--help)
      sed -n '2,15p' "$0"
      exit 0
      ;;
    *) echo "ERROR: unknown argument: $1" >&2; exit 1 ;;
  esac
done

case "$environment" in
  stage) account_id="904923506875"; profile="stage-blob-ops" ;;
  prod)  account_id="798406098447"; profile="prod-blob-ops" ;;
  *) echo "ERROR: --environment must be 'stage' or 'prod'" >&2; exit 1 ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$script_dir/.."

branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "main" ] || {
  echo "ERROR: must be run from 'main', not '${branch}'" >&2
  exit 1
}

[ -z "$(git status --porcelain)" ] || {
  echo "ERROR: working tree has uncommitted changes" >&2
  exit 1
}

git fetch origin main --quiet
local_head="$(git rev-parse HEAD)"
remote_head="$(git rev-parse origin/main)"
[ "$local_head" = "$remote_head" ] || {
  echo "ERROR: local main (${local_head}) does not match origin/main (${remote_head})" >&2
  echo "Run 'git pull' first." >&2
  exit 1
}

[ "${AWS_PROFILE:-}" = "$profile" ] || {
  echo "ERROR: AWS_PROFILE must be '${profile}' for --environment ${environment} (got '${AWS_PROFILE:-unset}')" >&2
  exit 1
}

caller_account="$(aws sts get-caller-identity --query Account --output text)"
[ "$caller_account" = "$account_id" ] || {
  echo "ERROR: AWS_PROFILE=${profile} resolved to account ${caller_account}, expected ${account_id}" >&2
  exit 1
}

registry="${account_id}.dkr.ecr.${region}.amazonaws.com"
tag="main-$(git rev-parse --short=12 HEAD)"
image_ref="${registry}/privy/inngest:${tag}"

echo "Building and pushing ${image_ref} ..."

aws ecr get-login-password --region "$region" \
  | docker login --username AWS --password-stdin "$registry"

docker buildx build --platform linux/amd64,linux/arm64 \
  -t "$image_ref" \
  --push .

echo
echo "Pushed: ${image_ref}"
