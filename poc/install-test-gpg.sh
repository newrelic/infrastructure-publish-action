#!/bin/bash
# Installs the batch POC packages from the testing bucket in clean containers, with GPG checks ON,
# the way a customer would: signed apt Release / yum repomd.xml, signed packages, the agent and
# every other dependency installed for real.
# Reads only the testing bucket (public keys included); never production.
#
#   bash poc/install-test-gpg.sh [dest_prefix]      # default infrastructure_agent/batch_poc/
#   MANIFEST=batch-manifest.json bash poc/install-test-gpg.sh "$DEST_PREFIX"   # the batch's apps
#
# Per container, two steps:
#   1. source = shared testing repo (infrastructure_agent/linux): install the agent and nrjmx, the
#      dependencies of the integrations. Under a POC dest_prefix the batch index lists them too,
#      but their files stay in the shared repo, so they come from there. Their package scripts are
#      skipped: they start a service and a container has no systemd.
#   2. source = POC repo only: install the batch's packages normally, dependencies resolved by
#      apt/dnf, and check each one was downloaded from the POC repo.
# If the shared repo has no agent/nrjmx for a distribution, step 2 can't resolve dependencies: that
# is reported as WARN (it depends on what the testing repo holds, not on the batch) and the
# packages are installed without them, so they are still tested.
#
# Needs docker. Runs linux/amd64 images (emulated on Apple silicon, slower).
set -uo pipefail

HOST=http://nr-downloads-ohai-testing.s3-website-us-east-1.amazonaws.com
PREFIX=${1:-infrastructure_agent/batch_poc/}
BASE="$HOST/${PREFIX}linux"
SHARED="$HOST/infrastructure_agent/linux"
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

# the agent and nrjmx a set depends on
deps_for() { case "$1" in *-fips*) echo "newrelic-infra-fips nrjmx-fips" ;; *) echo "newrelic-infra nrjmx" ;; esac; }

# The armored public key is mounted into the containers, so nothing has to be installed from the
# distributions' own mirrors (old releases' mirrors move or disappear).
KEYDIR=$(mktemp -d)
trap 'rm -rf "$KEYDIR"' EXIT
curl -sf --retry 3 -o "$KEYDIR/newrelic.asc" "$KEY" || { echo "cannot download $KEY"; exit 1; }
# apt verifies signatures as the unprivileged _apt user: the mounted key must be world-readable
chmod 755 "$KEYDIR"; chmod 644 "$KEYDIR/newrelic.asc"

# apt in containers, signed-by the New Relic key.
#   SIGN=strict  apt verifies InRelease with the key (fails on a bad or missing signature)
#   SIGN=trusted signature not checked by apt (used when the distribution rejects the key itself)
apt_test() {
  local img="$1" pkgs="$2" sign="$3" shared_ok="$4" opt="signed-by=/keys/newrelic.asc" bad='^(E|W):'
  [ "$sign" = trusted ] && { opt="trusted=yes"; bad='^E:'; }
  docker run --rm --platform linux/amd64 -v "$KEYDIR:/keys:ro" "$img" bash -ec "
    export DEBIAN_FRONTEND=noninteractive
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
    . /etc/os-release
    # no services in a container: the agent's scripts would try to start it
    printf '#!/bin/sh\nexit 101\n' > /usr/sbin/policy-rc.d && chmod +x /usr/sbin/policy-rc.d
    src() {
      echo \"deb [$opt] \$1 \$VERSION_CODENAME main\" > /etc/apt/sources.list.d/nr.list
      apt-get update -q 2>&1 | tee /tmp/update.log
      # not '! grep': bash -e ignores a negated command's failure
      if grep -qE '$bad' /tmp/update.log; then echo \"apt-get update reported errors for \$1\"; exit 1; fi
    }

    echo '== 1. agent + nrjmx from the shared testing repo'
    # the shared testing repo may not have this distribution at all (e.g. trixie)
    [ '$shared_ok' = yes ] && src '$SHARED/apt'
    for dep in $(deps_for "$pkgs"); do
      # a real candidate, not just a name other packages depend on
      if [ '$shared_ok' = yes ] && apt-cache policy \$dep | grep -q 'Candidate: [0-9]'; then
        (cd /tmp && apt-get download -q \$dep && dpkg --unpack \${dep}_*.deb && rm -f /var/lib/dpkg/info/\$dep.postinst && dpkg --configure \$dep) >/dev/null
        echo \"dep \$dep \$(dpkg-query -W -f='\${Version}' \$dep)\"
      else
        echo \"DEP_MISSING: \$dep\"
      fi
    done

    echo '== 2. the batch packages from the POC repo, dependencies resolved by apt'
    src '$BASE/apt'
    if apt-get install -y -q --no-install-recommends $pkgs >/tmp/install.log 2>&1; then
      echo 'DEPS_OK'
      # every batch package came from the POC repo
      for p in $pkgs; do grep -F '$PREFIX' /tmp/install.log | grep -q \" \$p \" || { echo \"not downloaded from the POC repo: \$p\"; exit 1; }; done
    else
      echo \"DEPS_UNRESOLVED: \$(grep -E '^ ' /tmp/install.log | head -5 | tr -s ' \\n' ' ')\"
      cd /tmp && apt-get download -q $pkgs
      for p in $pkgs; do dpkg -i --force-depends \${p}_*.deb >/dev/null 2>&1 || true; done
    fi
    for p in $pkgs; do dpkg -s \$p | grep -q '^Status: install ok installed' || { echo \"not installed: \$p\"; exit 1; }; done
  "
}

report() {
  local what="$1" log="$2"
  pass "$what"
  local missing; missing=$(sed -n 's/^DEP_MISSING: //p' <<<"$log" | tr '\n' ' ')
  [ -n "$missing" ] && warn "$what: not in the shared testing repo: $missing"
  if grep -q '^DEPS_UNRESOLVED:' <<<"$log"; then
    warn "$what: dependencies not resolvable, installed without them:$(sed -n 's/^DEPS_UNRESOLVED://p' <<<"$log")"
  fi
}

for IMG in ubuntu:noble ubuntu:jammy ubuntu:focal debian:trixie debian:bookworm debian:bullseye; do
  codename=$(docker run --rm --platform linux/amd64 "$IMG" bash -c '. /etc/os-release; echo $VERSION_CODENAME')
  shared_ok=no
  curl -sfo /dev/null "$SHARED/apt/dists/$codename/Release" && shared_ok=yes
  for PKGS in "${SETS[@]}"; do
    [ $shared_ok = yes ] || warn "apt $IMG: the shared testing repo has no $codename, so no agent/nrjmx to install first"
    LOG=$(apt_test "$IMG" "$PKGS" strict $shared_ok 2>&1); rc=$?
    if [ $rc -ne 0 ] && grep -q 'SHA1 is not considered secure' <<<"$LOG"; then
      # Debian 13's apt (sqv) refuses the New Relic key: its self-signature uses SHA1. Same key as
      # production, so not a batch issue; install anyway to test the packages themselves.
      warn "apt $IMG: apt rejects the New Relic signing key (SHA1 self-signature); retrying without apt's signature check"
      LOG=$(apt_test "$IMG" "$PKGS" trusted $shared_ok 2>&1); rc=$?
    fi
    if [ $rc -eq 0 ]; then
      report "apt $IMG: $PKGS" "$LOG"
    else
      fail "apt $IMG: $PKGS"; tail -5 <<<"$LOG" | sed 's/^/      /'
    fi
  done
done

for V in 8 9; do
  for PKGS in "${SETS[@]}"; do
    # dnf verifies the repomd.xml signature (repo_gpgcheck) and every package signature (gpgcheck).
    # Only New Relic repos are used (no distribution mirrors).
    LOG=$(docker run --rm --platform linux/amd64 "rockylinux:$V" bash -ec "
      repo() { printf '[nr]\nname=nr\nbaseurl=%s/yum/el/$V/x86_64/\ngpgkey=$RPM_KEY\ngpgcheck=1\nrepo_gpgcheck=1\nenabled=1\n' \"\$1\" > /etc/yum.repos.d/nr.repo; dnf -q -y --disablerepo='*' --enablerepo=nr clean all >/dev/null; dnf -q -y --disablerepo='*' --enablerepo=nr makecache; }
      DNF=\"dnf -q -y --disablerepo=* --enablerepo=nr\"

      echo '== 1. agent + nrjmx from the shared testing repo'
      repo '$SHARED'
      for dep in $(deps_for "$PKGS"); do
        # the agent's scriptlets need systemd
        if dnf -q --disablerepo='*' --enablerepo=nr list --available \$dep >/dev/null 2>&1; then
          \$DNF --setopt=tsflags=noscripts install \$dep >/dev/null; echo \"dep \$dep \$(rpm -q --qf '%{VERSION}' \$dep)\"
        else
          echo \"DEP_MISSING: \$dep\"
        fi
      done

      echo '== 2. the batch packages from the POC repo'
      repo '$BASE'
      mkdir /tmp/p && cd /tmp/p
      for url in \$(dnf -q --disablerepo='*' --enablerepo=nr repoquery --location $PKGS); do curl -sfO \"\$url\"; done
      [ \$(ls *.rpm | wc -l) -eq $(wc -w <<<"$PKGS") ] || { echo 'packages missing from the repo metadata'; exit 1; }
      rpm --import '$RPM_KEY'
      # not '! grep': bash -e ignores a negated command's failure
      if rpm -K *.rpm | grep -v 'signatures OK'; then echo 'unsigned or badly signed package'; exit 1; fi
      # dependencies resolved by dnf: already installed, or from the POC repo
      if \$DNF install ./*.rpm >/tmp/install.log 2>&1; then
        echo 'DEPS_OK'
      else
        echo \"DEPS_UNRESOLVED: \$(grep -iE 'nothing provides|requires' /tmp/install.log | head -3 | tr -s ' \\n' ' ')\"
        rpm -i --nodeps *.rpm
      fi
      rpm -q $PKGS
    " 2>&1); rc=$?
    if [ $rc -eq 0 ]; then
      report "yum el$V: $PKGS" "$LOG"
    else
      fail "yum el$V: $PKGS"; tail -5 <<<"$LOG" | sed 's/^/      /'
    fi
  done
done

exit $FAILED
