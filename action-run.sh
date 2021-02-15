#!/bin/bash
set -e
# build docker image form Dockerfile
echo "Build fresh docker image for newrelic/infrastructure-publish-action"
# @TODO add --no-cache
docker build  -t newrelic/infrastructure-publish-action -f $GITHUB_ACTION_PATH/Dockerfile $GITHUB_ACTION_PATH

# run docker container to perform all actions inside
echo "Run docker container with action logic inside"
docker run --rm -it \
        --name=infrastructure-publish-action\
        --security-opt apparmor:unconfined \
        --device /dev/fuse \
        --cap-add SYS_ADMIN \
        -e AWS_SECRET_ACCESS_KEY \
        -e AWS_ACCESS_KEY \
        -e AWS_S3_BUCKET_NAME \
        -e REPO_NAME \
        -e APP_NAME \
        -e TAG \
        -e ARTIFACTS_DEST_FOLDER=/mnt/s3 \
        -e ARTIFACTS_SRC_FOLDER=/home/gha/assets \
        -e SCHEMA \
        -e SCHEMA_URL \
        -e ENV \
        -e GPG_PRIVATE_KEY_BASE64 \
        -e GPG_PASSPHRASE \
        -e GPG_KEY_NAME \
        -e GPG_KEY_RING=/home/gha/keyring.gpg \
        newrelic/infrastructure-publish-action
