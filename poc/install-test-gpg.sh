#!/bin/bash
# Installs the batch POC packages from the testing bucket in clean containers, with GPG checks ON,
# the way a customer would: signed apt Release / yum repomd.xml, signed packages, dependencies
# resolvable from the published repo.
# Reads only the testing bucket (public keys included); never production.
#
#   bash poc/install-test-gpg.sh [dest_prefix]      # default infrastructure_agent/batch_poc/
#   MANIFEST=batch-manifest.json bash poc/install-test-gpg.sh "$DEST_PREFIX"   # the batch's apps
#
# Needs docker. Runs linux/amd64 images (emulated on Apple silicon, slower).
set -uo pipefail

HOST=http://nr-downloads-ohai-testing.s3-website-us-east-1.amazonaws.com
PREFIX=${1:-infrastructure_agent/batch_poc/}
BASE="$HOST/${PREFIX}linux"
KEY="$HOST/infrastructure_agent/keys/newrelic_apt_key_current.gpg"
RPM_KEY="$HOST/infrastructure_agent/keys/newrelic_rpm_key_current.gpg"

# fips and non-fips packages install separately: each set depends on its own agent.
# MANIFEST (the batch manifest) picks the apps; without it, the workflow's default integrations.
if [ -n "${MANIFEST:-}" ]; then
  SETS=()
  for filter in 'select(endswith("-fips") | not)' 'select(endswith("-fips"))'; do
    set_pkgs=$(jq -r ".[].app_name | $filter" "$MANIFEST" | tr '\n' ' ')
    [ -n "${set_pkgs// /}" ] && SETS+=("${set_pkgs% }")
  done
else
  SETS=("nri-apache nri-kafka nri-mysql" "nri-apache-fips nri-kafka-fips nri-mysql-fips")
fi
FAILED=0
pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1"; FAILED=1; }

for IMG in ubuntu:noble ubuntu:jammy ubuntu:focal debian:trixie debian:bookworm debian:bullseye; do
  for PKGS in "${SETS[@]}"; do
    # apt-get verifies the Release signature (signed-by) and every package hash; --download-only
    # resolves all dependencies (agent, nrjmx) from the POC repo without running the agent's
    # systemd postinst, then dpkg installs the integrations themselves.
    if docker run --rm --platform linux/amd64 "$IMG" bash -ec "
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -qq >/dev/null && apt-get install -qq -y curl gnupg ca-certificates >/dev/null
      curl -sf '$KEY' | gpg --dearmor > /usr/share/keyrings/newrelic.gpg
      . /etc/os-release
      echo \"deb [signed-by=/usr/share/keyrings/newrelic.gpg] $BASE/apt \$VERSION_CODENAME main\" > /etc/apt/sources.list.d/poc.list
      apt-get update -qq -o Dir::Etc::sourcelist=/etc/apt/sources.list.d/poc.list -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0
      apt-get install -qq -y --download-only $PKGS >/dev/null
      cd /var/cache/apt/archives && for p in $PKGS; do dpkg -i --force-depends \${p}_*.deb >/dev/null; done
      dpkg -l $PKGS | grep -c '^ii' | grep -qx $(echo $PKGS | wc -w)
    " >/tmp/poc-install.log 2>&1; then
      pass "apt $IMG: $PKGS"
    else
      fail "apt $IMG: $PKGS"; tail -5 /tmp/poc-install.log | sed 's/^/      /'
    fi
  done
done

for V in 8 9; do
  for PKGS in "${SETS[@]}"; do
    # dnf checks the repomd.xml signature (repo_gpgcheck) and each package signature (gpgcheck)
    if docker run --rm --platform linux/amd64 "rockylinux:$V" bash -ec "
      printf '[poc]\nname=poc\nbaseurl=$BASE/yum/el/$V/x86_64/\ngpgkey=$RPM_KEY\ngpgcheck=1\nrepo_gpgcheck=1\nenabled=1\n' > /etc/yum.repos.d/poc.repo
      dnf -q -y --disablerepo='*' --enablerepo=poc makecache
      dnf -q -y --disablerepo='*' --enablerepo=poc download --destdir /tmp/p $PKGS
      rpm --import '$RPM_KEY'
      rpm -K /tmp/p/*.rpm && ! rpm -K /tmp/p/*.rpm | grep -vq 'signatures OK'
      rpm -i --nodeps /tmp/p/*.rpm
      rpm -q $PKGS >/dev/null
    " >/tmp/poc-install.log 2>&1; then
      pass "yum el$V: $PKGS"
    else
      fail "yum el$V: $PKGS"; tail -5 /tmp/poc-install.log | sed 's/^/      /'
    fi
  done
done

exit $FAILED
