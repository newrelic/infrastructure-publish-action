#!/bin/bash

# validate inputs
if [ "$AWS_ACCESS_KEY_ID" = "" ]; then
  echo "missing AWS_ACCESS_KEY_ID"
  exit 1
fi
if [ "$AWS_SECRET_ACCESS_KEY" = "" ]; then
  echo "missing AWS_SECRET_ACCESS_KEY"
  exit 1
fi
if [ "$AWS_REGION" = "" ]; then
  echo "missing AWS_REGION"
  exit 1
fi
if [ "$AWS_REGION" = "" ]; then
  echo "missing AWS_REGION"
  exit 1
fi
if [ "$AWS_ROLE_SESSION_NAME" = "" ]; then
  echo "missing AWS_ROLE_SESSION_NAME"
  exit 1
fi
if [ "$AWS_ROLE_ARN" = "" ]; then
  echo "missing AWS_ROLE_ARN"
  exit 1
fi
if [ "$AWS_S3_BUCKET_NAME" = "" ]; then
  echo "missing AWS_S3_BUCKET_NAME"
  exit 1
fi
if [ "$AWS_S3_MOUNT_DIRECTORY" = "" ]; then
  echo "missing AWS_S3_MOUNT_DIRECTORY"
  exit 1
fi

export AWS_PAGER=""

STS=($(aws sts assume-role \
		--role-arn "$AWS_ROLE_ARN" \
		--role-session-name "$AWS_ROLE_SESSION_NAME" \
		--query '[Credentials.AccessKeyId,Credentials.SecretAccessKey,Credentials.SessionToken]' \
		--output text))

# overwrite AWS key pair credentials with STS negotiated ones.
export AWS_ACCESS_KEY_ID="${STS[0]}"
export AWS_SECRET_ACCESS_KEY="${STS[1]}"
export AWS_SESSION_TOKEN="${STS[2]}"

# verify role successfully assumed
#aws sts get-caller-identity || (echo "cannot assume IAM role"; exit 1)
checkfile=deleteme.$(date +%d-%m-%Y_%H-%M-%S)
touch "${checkfile}"
aws s3 cp "${checkfile}" "s3://${AWS_S3_BUCKET_NAME}/"
aws s3 ls "s3://${AWS_S3_BUCKET_NAME}/"
aws s3 rm "s3://${AWS_S3_BUCKET_NAME}/${checkfile}"

# s3fs token issue: https://github.com/s3fs-fuse/s3fs-fuse/issues/651#issuecomment-561111268
AWSACCESSKEYID="${AWS_ACCESS_KEY_ID}" \
AWSSECRETACCESSKEY="${AWS_SECRET_ACCESS_KEY}" \
AWSSESSIONTOKEN="${AWS_SESSION_TOKEN}" \
s3fs "${AWS_S3_BUCKET_NAME}" "${AWS_S3_MOUNT_DIRECTORY}"
