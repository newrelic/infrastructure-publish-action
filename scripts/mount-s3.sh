#!/bin/bash

set -e

# expects AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_ROLE_ARN and AWS_ROLE_SESSION_NAME to be previously setup in env vars
# KST will be an array
KST=($(aws sts assume-role --role-arn "$AWS_ROLE_ARN" --role-session-name "$AWS_ROLE_SESSION_NAME" --query '[Credentials.AccessKeyId,Credentials.SecretAccessKey,Credentials.SessionToken]' --output text))

aws configure set aws_region $AWS_REGION --profile temp
aws configure set aws_access_key_id "${KST[0]}" --profile temp
aws configure set aws_secret_access_key "${KST[1]}" --profile temp
aws configure set aws_session_token "${KST[2]}" --profile temp

cat ~/.aws/config
cat ~/.aws/credentials

#export AWS_ACCESS_KEY_ID="${KST[0]}"
#export AWS_SECRET_ACCESS_KEY="${KST[1]}"
#export AWS_SESSION_TOKEN="${KST[2]}"
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_SESSION_TOKEN

echo "Mounting S3 bucket $AWS_S3_BUCKET_NAME) into $AWS_S3_MOUNT_DIRECTORY..."
goofys --profile temp $AWS_S3_BUCKET_NAME $AWS_S3_MOUNT_DIRECTORY

