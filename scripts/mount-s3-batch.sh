#!/bin/bash

set -e

# Mounts the bucket like mount-s3.sh, but asks STS for a longer session: a batch can run for hours and the
# default 1 hour credentials of the mount expire in the middle of it ("Transport endpoint is not connected").
# AWS_SESSION_DURATION_SECONDS (default 14400, 4 hours) can not be longer than the maximum session duration of
# the role. If the role does not allow it, the default session is requested instead and a warning is printed.
# expects AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_ROLE_ARN and AWS_ROLE_SESSION_NAME to be previously setup in env vars
DURATION="${AWS_SESSION_DURATION_SECONDS:-14400}"

assume_role() {
  aws sts assume-role --role-arn "$AWS_ROLE_ARN" --role-session-name "$AWS_ROLE_SESSION_NAME" "$@" \
    --query '[Credentials.AccessKeyId,Credentials.SecretAccessKey,Credentials.SessionToken]' --output text
}

if ! CREDENTIALS=$(assume_role --duration-seconds "$DURATION" 2>/tmp/assume-role.err); then
  echo "WARNING: cannot assume the role for ${DURATION}s ($(tr '\n' ' ' </tmp/assume-role.err)), using the default session duration"
  CREDENTIALS=$(assume_role)
fi
# KST will be an array
KST=($CREDENTIALS)

aws configure set aws_region $AWS_REGION --profile temp
aws configure set aws_access_key_id "${KST[0]}" --profile temp
aws configure set aws_secret_access_key "${KST[1]}" --profile temp
aws configure set aws_session_token "${KST[2]}" --profile temp

unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_SESSION_TOKEN

echo "Mounting S3 bucket $AWS_S3_BUCKET_NAME into $AWS_S3_MOUNT_DIRECTORY..."
goofys --profile temp $AWS_S3_BUCKET_NAME $AWS_S3_MOUNT_DIRECTORY
