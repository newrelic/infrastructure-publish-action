#!/bin/bash
# Upgrade test for a batch publish, in clean containers, GPG checks ON:
#   1. install the integrations' previous versions (and the agent / nrjmx they depend on) from the
#      shared testing repo, the repo a host already uses
#   2. switch the host to the batch's repo (dest_prefix) and refresh its signed metadata
#   3. upgrade the integrations through it and check the batch's versions got installed
# so it shows a host moving from the existing repo to the batch-published metadata keeps working.
# Reads only the testing bucket (public keys included); never production.
#
#   bash poc/upgrade-test.sh [dest_prefix]          # default infrastructure_agent/batch_poc/
#   MANIFEST=batch-manifest.json bash poc/upgrade-test.sh "$DEST_PREFIX"
#
# An app is SKIPped on a distribution when the shared repo has no older version of it, or not its
# dependencies, to start from (e.g. -fips integrations, newer than anything in testing).
# The agent's package scripts are skipped (no systemd in a container); the integrations' run.
# Needs docker. Runs linux/amd64 images (emulated on Apple silicon, slower).
set -uo pipefail

HOST=http://nr-downloads-ohai-testing.s3-website-us-east-1.amazonaws.com
PREFIX=${1:-infrastructure_agent/batch_poc/}
BATCH="$HOST/${PREFIX}linux"
SHARED="$HOST/infrastructure_agent/linux"
KEY="$HOST/infrastructure_agent/keys/newrelic_apt_key_current.gpg"
RPM_KEY="$HOST/infrastructure_agent/keys/newrelic_rpm_key_current.gpg"

# "app=version" for every app of the batch
if [ -n "${MANIFEST:-}" ]; then
  APPS=$(jq -r '.[] | "\(.app_name)=\(.app_version // (.tag | ltrimstr("v")))"' "$MANIFEST" | tr '\n' ' ')
else
  APPS="nri-apache=1.18.2 nri-kafka=3.27.0 nri-mysql=1.25.0 nri-apache-fips=1.18.2 nri-kafka-fips=3.27.0 nri-mysql-fips=1.25.0"
fi
FAILED=0
pass() { echo "PASS  $1"; }
fail() { echo "FAIL  $1"; FAILED=1; }
skip() { echo "SKIP  $1"; }

KEYDIR=$(mktemp -d)
trap 'rm -rf "$KEYDIR"' EXIT
curl -sf --retry 3 -o "$KEYDIR/newrelic.asc" "$KEY" || { echo "cannot download $KEY"; exit 1; }
# apt verifies signatures as the unprivileged _apt user: the mounted key must be world-readable
chmod 755 "$KEYDIR"; chmod 644 "$KEYDIR/newrelic.asc"

# apps of the batch the shared apt index of a distribution has an older version of
apt_candidates() {
  local codename="$1" index app
  index=$(curl -sf "$SHARED/apt/dists/$codename/main/binary-amd64/Packages.gz" | gzip -dc 2>/dev/null) || return
  for app in $APPS; do
    grep -qx "Package: ${app%%=*}" <<<"$index" && echo "$app"
  done
}

for IMG in ubuntu:noble ubuntu:jammy ubuntu:focal debian:trixie debian:bookworm debian:bullseye; do
  codename=$(docker run --rm --platform linux/amd64 "$IMG" bash -c '. /etc/os-release; echo $VERSION_CODENAME')
  CANDIDATES=$(apt_candidates "$codename" | tr '\n' ' ')
  for app in $APPS; do
    grep -qw "$app" <<<"$CANDIDATES" || skip "apt $IMG: ${app%%=*} (no older version in the shared testing repo)"
  done
  [ -n "${CANDIDATES// /}" ] || continue
  PKGS=$(for a in $CANDIDATES; do echo -n "${a%%=*} "; done)

  LOG=$(docker run --rm --platform linux/amd64 -v "$KEYDIR:/keys:ro" "$IMG" bash -ec "
    export DEBIAN_FRONTEND=noninteractive
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
    # no services in a container: the agent's scripts would try to start it
    printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d && chmod +x /usr/sbin/policy-rc.d
    src() { echo \"deb [signed-by=/keys/newrelic.asc] \$1 $codename main\" > /etc/apt/sources.list.d/nr.list; apt-get update -q 2>&1 | tee /tmp/update.log; if grep -qE '^(E|W):' /tmp/update.log; then echo \"apt-get update reported errors for \$1\"; exit 1; fi; }

    echo '== 1. previous versions from the shared testing repo'
    src '$SHARED/apt'
    # the agent first, without its maintainer scripts; then the integrations, normally
    for dep in newrelic-infra nrjmx; do
      # a real candidate, not just a name other packages depend on (noble's index has no nrjmx)
      if apt-cache policy \$dep | grep -q 'Candidate: [0-9]'; then (cd /tmp && apt-get download -q \$dep && dpkg --unpack \${dep}_*.deb && rm -f /var/lib/dpkg/info/\$dep.postinst && dpkg --configure \$dep) >/dev/null; fi
    done
    OK=''
    for p in $PKGS; do
      if apt-get install -s -q --no-install-recommends \$p >/dev/null 2>&1; then OK=\"\$OK \$p\"; else echo \"nodeps \$p\"; fi
    done
    [ -n \"\$OK\" ] || exit 0
    apt-get install -q -y --no-install-recommends \$OK >/dev/null
    for p in \$OK; do echo \"before \$p \$(dpkg-query -W -f='\${Version}' \$p)\"; done

    echo '== 2. switch to the batch repo'
    src '$BATCH/apt'
    for p in \$OK; do echo \"policy \$p \$(apt-cache policy \$p | sed -n 's/ *Candidate: //p') (\$(apt-cache madison \$p | wc -l) versions in index)\"; done

    echo '== 3. upgrade through the batch metadata'
    apt-get install -q -y --only-upgrade \$OK >/dev/null
    for p in \$OK; do echo \"after \$p \$(dpkg-query -W -f='\${Version}' \$p)\"; done
  " 2>&1); rc=$?

  for a in $CANDIDATES; do
    app=${a%%=*}; want=${a#*=}
    before=$(sed -n "s/^before $app //p" <<<"$LOG"); after=$(sed -n "s/^after $app //p" <<<"$LOG")
    if grep -qx "nodeps $app" <<<"$LOG"; then
      skip "apt $IMG: $app (its dependencies are not in the shared testing repo)"
    elif [ "${before%-1}" = "$want" ]; then
      skip "apt $IMG: $app (the shared testing repo already has $want)"
    elif [ $rc -eq 0 ] && [ "${after%-1}" = "$want" ] && [ -n "$before" ]; then
      pass "apt $IMG: $app $before -> $after ($(sed -n "s/^policy $app [^(]*(\(.*\))/\1/p" <<<"$LOG"))"
    else
      fail "apt $IMG: $app ${before:-not installed} -> ${after:-not upgraded}, wanted $want"
    fi
  done
  [ $rc -eq 0 ] || tail -6 <<<"$LOG" | sed 's/^/      /'
done

for V in 8 9; do
  primary=$(curl -sf "$SHARED/yum/el/$V/x86_64/repodata/repomd.xml" | grep -o 'repodata/[^"]*primary.xml[^"]*' | head -1)
  NAMES=$(curl -sf "$SHARED/yum/el/$V/x86_64/$primary" | gzip -dc 2>/dev/null | grep -o '<name>[^<]*</name>' | sort -u)
  CANDIDATES=""
  for app in $APPS; do
    if grep -qx "<name>${app%%=*}</name>" <<<"$NAMES"; then CANDIDATES+="$app "; else skip "yum el$V: ${app%%=*} (no older version in the shared testing repo)"; fi
  done
  [ -n "${CANDIDATES// /}" ] || continue
  PKGS=$(for a in $CANDIDATES; do echo -n "${a%%=*} "; done)

  LOG=$(docker run --rm --platform linux/amd64 "rockylinux:$V" bash -ec "
    repo() { printf '[nr]\nname=nr\nbaseurl=%s/yum/el/$V/x86_64/\ngpgkey=$RPM_KEY\ngpgcheck=1\nrepo_gpgcheck=1\nenabled=1\n' \"\$1\" > /etc/yum.repos.d/nr.repo; dnf -q -y --disablerepo='*' --enablerepo=nr clean all >/dev/null; dnf -q -y --disablerepo='*' --enablerepo=nr makecache; }
    DNF=\"dnf -q -y --disablerepo=* --enablerepo=nr\"

    echo '== 1. previous versions from the shared testing repo'
    repo '$SHARED'
    # the agent's scriptlets need systemd; the integrations' run normally
    \$DNF --setopt=tsflags=noscripts install newrelic-infra \$(dnf -q --disablerepo='*' --enablerepo=nr list --available nrjmx >/dev/null 2>&1 && echo nrjmx)
    \$DNF install $PKGS
    for p in $PKGS; do echo \"before \$p \$(rpm -q --qf '%{VERSION}' \$p)\"; done

    echo '== 2. switch to the batch repo'
    repo '$BATCH'

    echo '== 3. upgrade through the batch metadata'
    \$DNF upgrade $PKGS
    for p in $PKGS; do echo \"after \$p \$(rpm -q --qf '%{VERSION}' \$p)\"; done
  " 2>&1); rc=$?

  for a in $CANDIDATES; do
    app=${a%%=*}; want=${a#*=}
    before=$(sed -n "s/^before $app //p" <<<"$LOG"); after=$(sed -n "s/^after $app //p" <<<"$LOG")
    if [ "$before" = "$want" ]; then
      skip "yum el$V: $app (the shared testing repo already has $want)"
    elif [ $rc -eq 0 ] && [ "$after" = "$want" ] && [ -n "$before" ]; then
      pass "yum el$V: $app $before -> $after"
    else
      fail "yum el$V: $app ${before:-not installed} -> ${after:-not upgraded}, wanted $want"
    fi
  done
  [ $rc -eq 0 ] || tail -6 <<<"$LOG" | sed 's/^/      /'
done

exit $FAILED
