#!/bin/bash

# build docker image form Dockerfile
echo "Build fresh docker image for newrelic/infrastructure-publish-action"
# @TODO add --no-cache
docker build  -t newrelic/infrastructure-publish-action -f ./Dockerfile .

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
        newrelic/infrastructure-publish-action
