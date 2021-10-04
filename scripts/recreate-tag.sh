#!/usr/bin/env bash

TAG_NAME="v1"

git fetch --tags --prune-tags -f
git checkout origin/main
git tag -d $TAG_NAME
git push --delete origin $TAG_NAME
git tag $TAG_NAME
git push origin $TAG_NAME