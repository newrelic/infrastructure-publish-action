#!/bin/bash

set -eo pipefail

cat << EOF

Test publishing end-to-end (agent tarballs):
 * uses real schema from URL
 * publishes into testing S3 bucket
 * asserts assets were published on bucket
EOF

# Public (forwarded) env-vars
TAG=1.16.3
AWS_REGION="us-east-1"
AWS_S3_BUCKET_NAME="nr-downloads-ohai-testing"
AWS_S3_LOCK_BUCKET_NAME="onhost-ci-lock-testing"
AWS_ROLE_SESSION_NAME="caos_testing"
AWS_ROLE_ARN="arn:aws:iam::017663287629:role/caos_testing"
DEST_PREFIX="infrastructure_agent/test_e2e/$(uuidgen)/"
# Private vars
_ASSERT_FILES=()
_ASSERT_FILES+=("s3://${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}binaries/linux/386/newrelic-infra_linux_${TAG}")
_ASSERT_FILES+=("s3://${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}linux/apt/pool/main/n/newrelic-infra/newrelic-infra_systemd_${TAG}")
_ASSERT_FILES+=("s3://${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}linux/yum/el/6/x86_64/newrelic-infra-${TAG}")
_ASSERT_FILES+=("s3://${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}linux/zypp/sles/11.4/x86_64/newrelic-infra-${TAG}")
_ASSERT_FILES+=("s3://${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}windows/newrelic-infra.${TAG}")
_ASSERT_FILES+=("s3://${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}binaries/windows/amd64/newrelic-infra-amd64.${TAG}")

printf "\n- Verifying secrets are available through env-vars...\n"

if [ "$ROOT_DIR" == "" ]; then
  printf "\nError: missing env-var: ROOT_DIR\n"
  exit 1
fi

if [ "$GPG_KEY_NAME" == "" ]; then
  printf "\nError: missing env-var: GPG_KEY_NAME\n"
  exit 1
fi

if [ "$GPG_PRIVATE_KEY_BASE64" == "" ]; then
  printf "\nError: missing env-var: GPG_PRIVATE_KEY_BASE64\n"
  exit 1
fi

if [ "$GPG_PASSPHRASE" == "" ]; then
  printf "\nError: missing env-var: GPG_PASSPHRASE\n"
  exit 1
fi

if [ "$AWS_SECRET_ACCESS_KEY" == "" ]; then
  printf "\nError: missing env-var: AWS_SECRET_ACCESS_KEY\n"
  exit 1
fi

if [ "$DOCKER_HUB_ID" == "" ]; then
  printf "\nError: missing env-var: DOCKER_HUB_ID\n"
  exit 1
fi

if [ "$DOCKER_HUB_PASSWORD" == "" ]; then
  printf "\nError: missing env-var: DOCKER_HUB_PASSWORD\n"
  exit 1
fi

for _ASSERT_FILE_ABSOLUTE_PATH in "${_ASSERT_FILES[@]}";do
  _ASSERT_DIR=$( dirname "${_ASSERT_FILE_ABSOLUTE_PATH}" )
  printf "\n- Verifying TAG %s was not published into ${_ASSERT_DIR}...\n" "$TAG"

  aws s3 ls "${_ASSERT_FILE_ABSOLUTE_PATH}" \
  && printf '\nError: asset %s already exists!\n' "${_ASSERT_FILE_ABSOLUTE_PATH}" \
  && exit 1
done

printf "\n- Running action: pre-release tarballs...\n"

# TODO : change SCHEMA_URL to point to main branch before merge@

ENV=pre-release \
APP_NAME=newrelic-infra \
REPO_NAME=newrelic/infrastructure-agent \
RUN_ID="000" \
SCHEMA=custom \
SCHEMA_URL=https://raw.githubusercontent.com/newrelic/infrastructure-publish-action/test/e2e_multiple_platforms/schemas/e2e.yml \
GITHUB_ACTION_PATH="$ROOT_DIR" \
AWS_S3_MOUNT_DIRECTORY=/mnt/s3 \
TAG="$TAG" \
AWS_REGION="$AWS_REGION" \
AWS_S3_BUCKET_NAME="$AWS_S3_BUCKET_NAME" \
AWS_S3_LOCK_BUCKET_NAME="$AWS_S3_LOCK_BUCKET_NAME" \
AWS_ROLE_SESSION_NAME="$AWS_ROLE_SESSION_NAME" \
AWS_ROLE_ARN="$AWS_ROLE_ARN" \
DEST_PREFIX="$DEST_PREFIX" \
"${ROOT_DIR}/action-run.sh"

printf "\n * Action run finished.\n"


printf "\n- Asserting published assets exist...\n"

for _ASSERT_FILE_ABSOLUTE_PATH in "${_ASSERT_FILES[@]}";do
  _ASSERT_DIR=$( dirname "${_ASSERT_FILE_ABSOLUTE_PATH}" )
  printf "\n- Verifying TAG %s was published into ${_ASSERT_DIR}...\n" "$TAG"

  aws s3 ls "${_ASSERT_FILE_ABSOLUTE_PATH}" \
  || (printf '\nError: missing published asset: %s!\n' "${_ASSERT_FILE_ABSOLUTE_PATH}" && exit 1)
done

printf "\n- Tear down:\n"

printf "\n * Assuming role %s...\n" "$AWS_ROLE_SESSION_NAME"

_STS=($(aws sts assume-role --role-arn "${AWS_ROLE_ARN}" --role-session-name "${AWS_ROLE_SESSION_NAME}" --query '[Credentials.AccessKeyId,Credentials.SecretAccessKey,Credentials.SessionToken]' --output text))
aws configure set aws_region "${AWS_REGION}" --profile bucketRole
aws configure set aws_access_key_id "${_STS[0]}" --profile bucketRole
aws configure set aws_secret_access_key "${_STS[1]}" --profile bucketRole
aws configure set aws_session_token "${_STS[2]}" --profile bucketRole
unset _STS

printf "\n * Cleaning up files...\n"

aws --profile bucketRole s3 rm --recursive "s3://${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}" \
  || (printf '\nError: cannot clean up published files at %s!\n' "${AWS_S3_BUCKET_NAME}/${DEST_PREFIX}" && exit 1)
