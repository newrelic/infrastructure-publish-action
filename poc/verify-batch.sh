#!/bin/bash
# Checks, over HTTP, that every app of a batch manifest was published the way its schema says:
#   apt:      every distribution's index lists each app's package for every arch, the pool file
#             is reachable, every source entry is published unchanged, only the batch adds entries
#             and Release is signed
#   yum/zypp: every repository's metadata lists each app's package, the package and the .repo
#             file are reachable and repomd.xml is signed
#   file:     every file upload is reachable
#
# Env:
#   MANIFEST           batch manifest (JSON array: app_name, repo_name, tag, schema, ...)
#   SCHEMAS_DIR        folder with the baked-in schemas (schemas/ in this repo)
#   ACCESS_POINT_HOST  host the repos are served from
#   MIRROR_HOST        host the apt repos were mirrored from (same as ACCESS_POINT_HOST for testing)
#   DEST_PREFIX        prefix the batch was published into
#   GPG_KEYRING        keyring holding the signing key
#   RESULTS            file to write PASS/FAIL lines to
set -uo pipefail

: "${RESULTS:=verify-results.txt}"
: "${MIRROR_HOST:=$ACCESS_POINT_HOST}"
: > "$RESULTS"
FAILED=0
WORK=$(mktemp -d)

pass() { echo "PASS  $1" | tee -a "$RESULTS" >/dev/null; }
fail() { echo "FAIL  $1" | tee -a "$RESULTS"; FAILED=1; }
reachable() { curl -sfI -o /dev/null --retry 3 "$1"; }
fetch() { curl -sf --retry 3 -o "$2" "$1"; }

decompress() {
  case "$1" in
    *.gz) gzip -dc "$1" ;;
    *.xz) xz -dc "$1" ;;
    *.bz2) bzip2 -dc "$1" ;;
    *.zst) zstd -dcq "$1" ;;
    *) cat "$1" ;;
  esac
}

# Same placeholders as utils.ReplacePlaceholders
render() {
  local s="$1"
  s=${s//\{repo_name\}/$REPO}
  s=${s//\{app_name\}/$APP}
  s=${s//\{arch\}/$2}
  s=${s//\{tag\}/$TAG}
  s=${s//\{version\}/$VERSION}
  s=${s//\{dest_prefix\}/$DEST_PREFIX}
  s=${s//\{os_version\}/$3}
  s=${s//\{access_point_host\}/$MIRROR_HOST}
  echo "$s"
}

verify_signature() {
  if gpg --batch --no-default-keyring --keyring "$GPG_KEYRING" --verify "$2" "$1" 2>/dev/null; then
    pass "$3: signature valid"
  else
    fail "$3: signature INVALID"
  fi
}

check_apt_distribution() {
  local codename="$1" dists d
  dists="$ACCESS_POINT_HOST/${DEST_PREFIX}linux/apt/dists/$codename"
  d="$WORK/apt/$codename"
  [ -d "$d" ] && return # already checked for a previous app
  mkdir -p "$d"
  if ! fetch "$dists/Release" "$d/Release" || ! fetch "$dists/Release.gpg" "$d/Release.gpg"; then
    fail "apt $codename: Release missing"
    return
  fi
  verify_signature "$d/Release" "$d/Release.gpg" "apt $codename Release"

  : > "$d/Packages"
  for arch in $(sed -n 's/^Architectures: //p' "$d/Release"); do
    fetch "$dists/main/binary-$arch/Packages.gz" "$d/p.gz" && gzip -dc "$d/p.gz" >> "$d/Packages" && echo >> "$d/Packages"
  done

  # the mirrored packages must still be there
  local src="$MIRROR_HOST/infrastructure_agent/linux/apt/dists/$codename" before after
  if fetch "$src/Release" "$d/src-Release"; then
    : > "$d/src-Packages"
    for arch in $(sed -n 's/^Architectures: //p' "$d/src-Release"); do
      fetch "$src/main/binary-$arch/Packages.gz" "$d/s.gz" && gzip -dc "$d/s.gz" >> "$d/src-Packages" && echo >> "$d/src-Packages"
    done
    before=$(grep -c '^Package: ' "$d/src-Packages")
    after=$(grep -c '^Package: ' "$d/Packages")
    if [ "$after" -ge "$before" ]; then
      pass "apt $codename: mirrored packages kept ($before in source, $after published)"
    else
      fail "apt $codename: lost mirrored packages ($before in source, $after published)"
    fi

    # The index paths are relative to the repo root, so this metadata is only safe at the real path
    # if every source entry is published unchanged (same file, size and hash) and the only new
    # entries are the batch's own packages.
    stanza_keys "$d/src-Packages" > "$d/src-keys"
    stanza_keys "$d/Packages" > "$d/keys"
    local changed added
    changed=$(comm -23 "$d/src-keys" "$d/keys" | wc -l | tr -d ' ')
    added=$(comm -13 "$d/src-keys" "$d/keys" | cut -d'|' -f1 | sort -u | grep -vxF -f "$WORK/batch-apps" | tr '\n' ' ')
    if [ "$changed" -eq 0 ]; then
      pass "apt $codename: all $(wc -l < "$d/src-keys" | tr -d ' ') source entries published unchanged (filename, size, sha256)"
    else
      fail "apt $codename: $changed source entries missing or changed, e.g. $(comm -23 "$d/src-keys" "$d/keys" | head -2 | tr '\n' ' ')"
    fi
    if [ -z "${added// /}" ]; then
      pass "apt $codename: only the batch's packages added"
    else
      fail "apt $codename: entries added for packages outside the batch: $added"
    fi
  fi
}

# one "package|version|arch|filename|size|sha256" line per index entry; arch "all" entries repeat
# in every binary-<arch> index, hence the sort -u
stanza_keys() {
  awk '/^Package: /{p=$2} /^Version: /{v=$2} /^Architecture: /{a=$2} /^Filename: /{f=$2} /^Size: /{s=$2} /^SHA256: /{h=$2}
       /^$/ { if (p != "") print p "|" v "|" a "|" f "|" s "|" h; p = v = a = f = s = h = "" }
       END  { if (p != "") print p "|" v "|" a "|" f "|" s "|" h }' "$1" | sort -u
}

check_apt_app() {
  local codename="$1" expected="$2" d="$WORK/apt/$1" stanzas found=0 file missing=0
  [ -f "$d/Packages" ] || return
  # stanzas of this app at this version, one line each: "<filename>"
  stanzas=$(awk -v app="$APP" -v ver="$VERSION" '
    /^Package: /  { p = $2 } /^Version: / { v = $2 } /^Filename: / { f = $2 }
    /^$/ { if (p == app && (v == ver || v == ver "-1")) print f; p = v = f = "" }' "$d/Packages" | sort -u)
  for file in $stanzas; do
    found=$((found + 1))
    reachable "$ACCESS_POINT_HOST/${DEST_PREFIX}linux/apt/$file" || { missing=1; echo "  unreachable $file"; }
  done
  if [ "$found" -eq "$expected" ] && [ "$missing" -eq 0 ]; then
    pass "apt $codename: $APP $VERSION ($found archs)"
  else
    fail "apt $codename: $APP $VERSION found $found of $expected archs, unreachable=$missing"
  fi
}

check_rpm_repo() {
  # repo_path keeps the schema's trailing slash, as the .repo baseurl does
  local repo_path="$1" rpm_name="$2" url="$ACCESS_POINT_HOST/$1" base key d primary
  base="${url%/}"
  key=$(echo "$repo_path" | tr '/' '_')
  d="$WORK/rpm/$key"
  mkdir -p "$d"
  if [ ! -f "$d/repomd.xml" ]; then
    if ! fetch "$base/repodata/repomd.xml" "$d/repomd.xml"; then
      fail "rpm $repo_path: repomd.xml missing"; return
    fi
    if fetch "$base/repodata/repomd.xml.asc" "$d/repomd.xml.asc"; then
      verify_signature "$d/repomd.xml" "$d/repomd.xml.asc" "rpm $repo_path repomd.xml"
    else
      fail "rpm $repo_path: repomd.xml.asc missing"
    fi
    if fetch "$base/newrelic-infra.repo" "$d/newrelic-infra.repo" && grep -qx "baseurl=$url" "$d/newrelic-infra.repo"; then
      pass "rpm $repo_path: newrelic-infra.repo"
    else
      fail "rpm $repo_path: newrelic-infra.repo missing or wrong baseurl"
    fi
    primary=$(grep -o 'href="repodata/[^"]*primary.xml[^"]*"' "$d/repomd.xml" | head -1 | sed 's/href="//; s/"$//')
    fetch "$base/$primary" "$d/$(basename "$primary")" && decompress "$d/$(basename "$primary")" > "$d/primary.xml"
  fi
  if grep -q "<name>$APP</name>" "$d/primary.xml" 2>/dev/null && grep -q "href=\"$rpm_name\"" "$d/primary.xml" && reachable "$base/$rpm_name"; then
    pass "rpm $repo_path: $rpm_name"
  else
    fail "rpm $repo_path: $rpm_name not in metadata or unreachable"
  fi
}

APPS=$(jq length "$MANIFEST")
jq -r '.[].app_name' "$MANIFEST" > "$WORK/batch-apps"
for i in $(seq 0 $((APPS - 1))); do
  APP=$(jq -r ".[$i].app_name" "$MANIFEST")
  REPO=$(jq -r ".[$i].repo_name" "$MANIFEST")
  TAG=$(jq -r ".[$i].tag" "$MANIFEST")
  VERSION=$(jq -r ".[$i].app_version // empty" "$MANIFEST"); VERSION=${VERSION:-${TAG//v/}}
  SCHEMA=$(jq -r ".[$i].schema" "$MANIFEST")
  case "$SCHEMA" in
    custom) curl -sf -o "$WORK/schema-$APP.yml" "$(jq -r ".[$i].schema_url" "$MANIFEST")"; SCHEMA_FILE="$WORK/schema-$APP.yml" ;;
    custom-local) SCHEMA_FILE=$(jq -r ".[$i].schema_path" "$MANIFEST") ;;
    *) SCHEMA_FILE="$SCHEMAS_DIR/$SCHEMA.yml" ;;
  esac
  yq -o=json "$SCHEMA_FILE" > "$WORK/schema-$APP.json"
  echo "== $APP $TAG ($SCHEMA)"

  ENTRIES=$(jq length "$WORK/schema-$APP.json")
  for e in $(seq 0 $((ENTRIES - 1))); do
    SRC=$(jq -r ".[$e].src" "$WORK/schema-$APP.json")
    ARCHS=$(jq -r ".[$e].arch // [\"\"] | .[]" "$WORK/schema-$APP.json")
    ARCH_COUNT=$(jq ".[$e].arch // [\"\"] | length" "$WORK/schema-$APP.json")
    UPLOADS=$(jq ".[$e].uploads | length" "$WORK/schema-$APP.json")
    for u in $(seq 0 $((UPLOADS - 1))); do
      TYPE=$(jq -r ".[$e].uploads[$u].type" "$WORK/schema-$APP.json")
      DEST=$(jq -r ".[$e].uploads[$u].dest" "$WORK/schema-$APP.json")
      OS_VERSIONS=$(jq -r ".[$e].uploads[$u].os_version // [] | .[]" "$WORK/schema-$APP.json")
      case "$TYPE" in
        apt)
          for c in $OS_VERSIONS; do
            check_apt_distribution "$c"
            check_apt_app "$c" "$ARCH_COUNT"
          done ;;
        yum|zypp)
          for arch in $ARCHS; do
            dest_arch=$arch; [ "$arch" = arm64 ] && dest_arch=aarch64
            for os in $OS_VERSIONS; do
              check_rpm_repo "$(render "$DEST" "$dest_arch" "$os")" "$(render "$SRC" "$arch" "$os")"
            done
          done ;;
        file)
          for arch in $ARCHS; do
            for os in ${OS_VERSIONS:-""}; do
              file_dest=$(render "$DEST" "$arch" "$os"); file_dest=${file_dest//\{src\}/$(render "$SRC" "$arch" "$os")}
              if reachable "$ACCESS_POINT_HOST/$file_dest"; then pass "file $file_dest"; else fail "file $file_dest unreachable"; fi
            done
          done ;;
      esac
    done
  done
done

echo "$(grep -c '^PASS' "$RESULTS") passed, $(grep -c '^FAIL' "$RESULTS") failed"
grep '^FAIL' "$RESULTS"
rm -rf "$WORK"
exit $FAILED
