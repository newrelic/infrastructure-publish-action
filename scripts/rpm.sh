#!/bin/bash
set -e
#
#
# Mount S3 with S3Fuse and update YUM or ZYPP repo.
#
#

#######################################
#    UPLOAD TO S3, UPDATE METADATA    #
#######################################

echo "===> Importing GPG signature"
printf %s ${GPG_PRIVATE_KEY_BASE64} | base64 --decode | gpg --batch --import -

echo "===> Download RPM packages from GH"
for arch in "${ARCH_LIST[@]}"; do
  if [ $arch == 'x86_64' ]; then
    package_name="nri-${INTEGRATION}-${TAG:1}-${SUFIX}.${arch}.rpm"
  else
    package_name="nri-${INTEGRATION}-${TAG:1}-${arch}.rpm"
  fi
  echo "===> Download ${package_name} from GH"
  wget https://github.com/${REPO_FULL_NAME}/releases/download/${TAG}/${package_name}
done


if [ $arch == 'x86_64' ]; then
  package_name="nri-${INTEGRATION}-${TAG:1}-${SUFIX}.${arch}.rpm"
else
  package_name="nri-${INTEGRATION}-${TAG:1}-${arch}.rpm"
fi
LOCAL_REPO_PATH="${AWS_S3_MOUNTPOINT}${BASE_PATH}/${os_version}/${arch}"
echo "===> Creating local directory if not exists ${LOCAL_REPO_PATH}/repodata"
[ -d "${LOCAL_REPO_PATH}/repodata" ] || mkdir -p "${LOCAL_REPO_PATH}/repodata"
echo "===> Uploading ${package_name} to S3 in ${BASE_PATH}/${os_version}/${arch}"
cp ${package_name} ${LOCAL_REPO_PATH}
echo "===> Delete and recreate metadata for ${package_name}"
find ${LOCAL_REPO_PATH} -regex '^.*repodata' | xargs -n 1 rm -rf
time createrepo --update -s sha "${LOCAL_REPO_PATH}"
FILE="${LOCAL_REPO_PATH}/repodata/repomd.xml"
while [ ! -f $FILE ];do
   echo "===> Waiting repomd.xml exists..."
done
echo "===> Updating GPG metadata dettached signature in ${BASE_PATH}/${os_version}/${arch}"
gpg --batch --pinentry-mode=loopback --passphrase ${GPG_PASSPHRASE} --detach-sign --armor "${LOCAL_REPO_PATH}/repodata/repomd.xml"
