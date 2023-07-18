#!/bin/bash
set -e
# build docker image form Dockerfile
echo "Build fresh docker image for newrelic/infrastructure-publish-action"
# @TODO add --no-cache
docker build --platform linux/amd64 -t newrelic/infrastructure-publish-action -f $GITHUB_ACTION_PATH/Dockerfile $GITHUB_ACTION_PATH

if [ "${CI}" = "true" ]; then
  # avoid container network errors in GHA runners
  set +e
  echo "Creating iptables rule to drop invalid packages"
  sudo iptables -D INPUT -i eth0 -m state --state INVALID -j DROP 2>/dev/null
  sudo iptables -A INPUT -i eth0 -m state --state INVALID -j DROP
  set -e
fi

## Freeing space since 14GB are not enough anymore
if [ "${CI}" = "true" ]; then
  df -ih
  echo "Deleting android, dotnet, haskell, CodeQL, Python, swift to free up space"
  sudo rm -rf /usr/local/lib/android /usr/share/dotnet /usr/local/.ghcup /opt/hostedtoolcache/CodeQL /opt/hostedtoolcache/Python /usr/share/swift
  df -ih
fi


# run docker container to perform all actions inside.
# $( pwd ) is mounted on /srv to enable grabbing packages
# from the host machine instead of downloading them from GH,
# and therefore as LOCAL_PACKAGES_PATH will refer to path
# inside the docker container it should be `/srv/*`
echo "Run docker container with action logic inside"
docker run --platform linux/amd64 --rm \
        --name=infrastructure-publish-action\
        --security-opt apparmor:unconfined \
        --device /dev/fuse \
        --cap-add SYS_ADMIN \
        -v $( pwd ):/srv \
        -e AWS_REGION \
        -e AWS_ACCESS_KEY_ID \
        -e AWS_SECRET_ACCESS_KEY \
        -e AWS_ROLE_SESSION_NAME \
        -e AWS_ROLE_ARN \
        -e AWS_S3_BUCKET_NAME \
        -e AWS_S3_LOCK_BUCKET_NAME \
        -e REPO_NAME \
        -e APP_NAME \
        -e APP_VERSION \
        -e TAG \
        -e ACCESS_POINT_HOST \
        -e RUN_ID \
        -e ARTIFACTS_DEST_FOLDER=/mnt/s3 \
        -e ARTIFACTS_SRC_FOLDER=/home/gha/assets \
        -e SCHEMA \
        -e SCHEMA_URL \
        -e SCHEMA_PATH=$( realpath --canonicalize-missing "$SCHEMA_PATH" | sed -e "s|$PWD|/srv|" ) \
        -e GPG_PRIVATE_KEY_BASE64 \
        -e GPG_PASSPHRASE \
        -e DISABLE_LOCK \
        -e GPG_KEY_RING=/home/gha/keyring.gpg \
        -e DEST_PREFIX \
        -e LOCAL_PACKAGES_PATH \
        -e APT_SKIP_MIRROR \
        newrelic/infrastructure-publish-action \
        "$@"

# Verifying how much space is still available
echo "After running the upload command this is the current status"
df -ih
