#!/bin/bash
# Installs the batch POC packages from the testing bucket in clean containers, with GPG checks ON,
# the way a customer would: signed apt Release / yum repomd.xml, signed packages.
# Reads only the testing bucket (public keys included); never production.
#
#   bash poc/install-test-gpg.sh [dest_prefix]      # default infrastructure_agent/batch_poc/
#   MANIFEST=batch-manifest.json bash poc/install-test-gpg.sh "$DEST_PREFIX"   # the batch's apps
#
# Only the batch's own packages are downloaded and installed (dpkg/rpm, dependencies not
# installed): under a POC dest_prefix the apt index lists the mirrored packages (agent, nrjmx...)
# but their pool files stay in the shared repo, so they can't be downloaded from the POC repo.
# Whether the dependencies resolve from the index is reported as WARN, not FAIL: it depends on
# what the testing repo holds for each distribution, not on the batch.
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
warn() { echo "WARN  $1"; }

# The armored public key is mounted into the containers, so nothing has to be installed from the
# distributions' own mirrors (old releases' mirrors move or disappear).
KEYDIR=$(mktemp -d)
trap 'rm -rf "$KEYDIR"' EXIT
curl -sf --retry 3 -o "$KEYDIR/newrelic.asc" "$KEY" || { echo "cannot download $KEY"; exit 1; }
# apt verifies signatures as the unprivileged _apt user: the mounted key must be world-readable
chmod 755 "$KEYDIR"; chmod 644 "$KEYDIR/newrelic.asc"

# apt in containers: only the POC repo as source, signed-by the New Relic key.
#   SIGN=strict  apt verifies InRelease with the key (fails on a bad or missing signature)
#   SIGN=trusted signature not checked by apt (used when the distribution rejects the key itself)
apt_test() {
  local img="$1" pkgs="$2" sign="$3" opt="signed-by=/keys/newrelic.asc" bad='^(E|W):'
  [ "$sign" = trusted ] && { opt="trusted=yes"; bad='^E:'; }
  docker run --rm --platform linux/amd64 -v "$KEYDIR:/keys:ro" "$img" bash -ec "
    export DEBIAN_FRONTEND=noninteractive
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
    . /etc/os-release
    echo \"deb [$opt] $BASE/apt \$VERSION_CODENAME main\" > /etc/apt/sources.list.d/poc.list
    apt-get update -q 2>&1 | tee /tmp/update.log
    # not '! grep': bash -e ignores a negated command's failure
    if grep -qE '$bad' /tmp/update.log; then echo 'apt-get update reported errors'; exit 1; fi
    # dependencies resolvable from the index (simulation, nothing downloaded)
    apt-get install -s -q $pkgs >/tmp/deps.log 2>&1 ||
      echo \"DEPS_UNRESOLVED: \$(grep -E '^ ' /tmp/deps.log | head -5 | tr -s ' \\n' ' ')\"
    cd /tmp && apt-get download -q $pkgs
    for p in $pkgs; do dpkg -i --force-depends \${p}_*.deb >/dev/null 2>&1 || true; done
    for p in $pkgs; do dpkg -s \$p | grep -q '^Status: install ok installed' || { echo \"not installed: \$p\"; exit 1; }; done
  "
}

for IMG in ubuntu:noble ubuntu:jammy ubuntu:focal debian:trixie debian:bookworm debian:bullseye; do
  for PKGS in "${SETS[@]}"; do
    LOG=$(apt_test "$IMG" "$PKGS" strict 2>&1); rc=$?
    if [ $rc -ne 0 ] && grep -q 'SHA1 is not considered secure' <<<"$LOG"; then
      # Debian 13's apt (sqv) refuses the New Relic key: its self-signature uses SHA1. Same key as
      # production, so not a batch issue; install anyway to test the packages themselves.
      warn "apt $IMG: apt rejects the New Relic signing key (SHA1 self-signature); retrying without apt's signature check"
      LOG=$(apt_test "$IMG" "$PKGS" trusted 2>&1); rc=$?
    fi
    if [ $rc -eq 0 ]; then
      pass "apt $IMG: $PKGS"
      grep -q '^DEPS_UNRESOLVED:' <<<"$LOG" &&
        warn "apt $IMG: dependencies not resolvable from the index:$(sed -n 's/^DEPS_UNRESOLVED://p' <<<"$LOG")"
    else
      fail "apt $IMG: $PKGS"; tail -5 <<<"$LOG" | sed 's/^/      /'
    fi
  done
done

for V in 8 9; do
  for PKGS in "${SETS[@]}"; do
    # dnf verifies the repomd.xml signature (repo_gpgcheck); rpm -K every package signature.
    # Only the POC repo is used (no distribution mirrors): package URLs come from its metadata.
    if LOG=$(docker run --rm --platform linux/amd64 "rockylinux:$V" bash -ec "
      printf '[poc]\nname=poc\nbaseurl=$BASE/yum/el/$V/x86_64/\ngpgkey=$RPM_KEY\ngpgcheck=1\nrepo_gpgcheck=1\nenabled=1\n' > /etc/yum.repos.d/poc.repo
      dnf -q -y --disablerepo='*' --enablerepo=poc makecache
      mkdir /tmp/p && cd /tmp/p
      for url in \$(dnf -q --disablerepo='*' --enablerepo=poc repoquery --location $PKGS); do curl -sfO \"\$url\"; done
      [ \$(ls *.rpm | wc -l) -eq $(wc -w <<<"$PKGS") ] || { echo 'packages missing from the repo metadata'; exit 1; }
      rpm --import '$RPM_KEY'
      rpm -K *.rpm
      # not '! grep': bash -e ignores a negated command's failure
      if rpm -K *.rpm | grep -v 'signatures OK'; then echo 'unsigned or badly signed package'; exit 1; fi
      rpm -i --nodeps *.rpm
      rpm -q $PKGS
    " 2>&1); then
      pass "yum el$V: $PKGS"
    else
      fail "yum el$V: $PKGS"; tail -5 <<<"$LOG" | sed 's/^/      /'
    fi
  done
done

exit $FAILED
