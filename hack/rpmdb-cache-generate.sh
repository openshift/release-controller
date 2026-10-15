#!/usr/bin/env bash
#
# rpmdb-cache-generate.sh — populate a static rpmdb cache for GA z-stream releases.
#
# Usage:
#   hack/rpmdb-cache-generate.sh [MINOR...]
#   hack/rpmdb-cache-generate.sh 4.21 4.22          # specific minor versions
#   hack/rpmdb-cache-generate.sh                     # default: 4.12 through 4.22
#
# The script:
#   1. Enumerates GA z-stream tags from quay.io/openshift-release-dev/ocp-release
#   2. Skips versions already recorded in rpmdb-cache/metadata.json
#   3. For each new version, extracts rpmdb + extensions data into rpmdb-cache/data/
#   4. Updates metadata.json and rebuilds rpmdb-cache/rpmdb-cache.tar.zst
#
# Requirements: oc, skopeo, jq, python3, zstd

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE_DIR="${REPO_ROOT}/rpmdb-cache/data"
METADATA="${REPO_ROOT}/rpmdb-cache/metadata.json"
TARBALL="${REPO_ROOT}/rpmdb-cache/rpmdb-cache.tar.zst"
REGISTRY="quay.io/openshift-release-dev/ocp-release"
EXTENSIONS_PATH="usr/share/rpm-ostree/extensions/extensions.json"

DEFAULT_MINORS="4.12 4.13 4.14 4.15 4.16 4.17 4.18 4.19 4.20 4.21 4.22"
OTHER_ARCHES="aarch64 s390x ppc64le"

# extract_digest <tagname>
# Reads a release JSON from stdin and prints the sha256 hex digest for the
# named component image tag, or nothing if the tag is absent.
# NOTE: uses -c so that stdin remains available for the piped JSON; a heredoc
# with `python3 -` would consume stdin for the script itself.
extract_digest() {
    python3 -c "$(cat <<'PYEOF'
import sys, json
tagname = sys.argv[1]
data = json.load(sys.stdin)
for t in data['references']['spec']['tags']:
    if t['name'] == tagname:
        ref = t.get('from', {}).get('name', '')
        if '@sha256:' in ref:
            print(ref.split('@sha256:')[1])
        break
PYEOF
)" "$1"
}

mkdir -p "${CACHE_DIR}"

# On a fresh checkout, data/ is gitignored but the tarball is committed.
# Extract it so the rebuild at the end doesn't drop previously cached entries.
if [[ -f "${TARBALL}" && -z "$(ls -A "${CACHE_DIR}")" ]]; then
    echo "Bootstrapping cache from existing tarball..."
    tar --zstd -xf "${TARBALL}" -C "${CACHE_DIR}"
fi

if [[ ! -f "${METADATA}" ]]; then
    echo '{"generated":"","versions":{}}' > "${METADATA}"
fi

# Parse arguments
MINORS="${*:-${DEFAULT_MINORS}}"

for cmd in oc skopeo jq python3 zstd; do
    if ! command -v "${cmd}" &>/dev/null; then
        echo "ERROR: ${cmd} is required but not found in PATH" >&2
        exit 1
    fi
done

# Fetch all tags once (expensive call to quay)
echo "Fetching tag list from ${REGISTRY}..."
ALL_TAGS=$(skopeo list-tags "docker://${REGISTRY}" 2>/dev/null | jq -r '.Tags[]')

cached_count=0
new_count=0
error_count=0
symlink_count=0

for minor in ${MINORS}; do
    # Filter to GA tags: 4.Y.Z-x86_64 (no rc, ec, multi)
    ga_tags=$(echo "${ALL_TAGS}" | grep -E "^${minor}\.[0-9]+-x86_64$" | sort -V)
    tag_count=$(echo "${ga_tags}" | grep -c . || true)

    if [[ ${tag_count} -eq 0 ]]; then
        echo "  ${minor}: no GA tags found, skipping"
        continue
    fi

    echo ""
    echo "═══ ${minor} (${tag_count} GA releases) ═══"

    for tag in ${ga_tags}; do
        version="${tag%-x86_64}"

        # Skip if already in metadata
        if jq -e ".versions[\"${version}\"]" "${METADATA}" &>/dev/null; then
            cached_count=$((cached_count + 1))
            continue
        fi

        echo -n "  ${version}: "

        # Get release JSON to discover streams
        release_json=$(oc adm release info -o json "${REGISTRY}:${tag}" 2>/dev/null) || {
            echo "FAILED (release info)"
            error_count=$((error_count + 1))
            continue
        }

        # Discover machine-OS streams: find *coreos*-extensions tags with matching base
        streams=$(echo "${release_json}" | python3 -c "
import sys, json
data = json.load(sys.stdin)
tags = {t['name'] for t in data['references']['spec']['tags']}
streams = []
for name in sorted(tags):
    if not name.endswith('-extensions') or 'coreos' not in name:
        continue
    base = name.removesuffix('-extensions')
    if base and base in tags:
        streams.append(base)
if streams:
    print(' '.join(streams))
" 2>/dev/null) || {
            echo "FAILED (stream discovery)"
            error_count=$((error_count + 1))
            continue
        }

        if [[ -z "${streams}" ]]; then
            echo "no coreos streams found, skipping"
            continue
        fi

        echo -n "streams=[${streams}] "
        stream_list_json="[]"

        for stream in ${streams}; do
            ext_tag="${stream}-extensions"

            # Populate sha256_* via --rpmdb-cache
            if ! oc adm release info --rpmdb --rpmdb-cache="${CACHE_DIR}/" \
                    --rpmdb-image="${stream}" --output=json \
                    "${REGISTRY}:${tag}" >/dev/null 2>&1; then
                echo -n "(rpmdb FAIL for ${stream}) "
                continue
            fi

            # Resolve extensions image pullspec from release JSON
            ext_pull=$(echo "${release_json}" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for t in data['references']['spec']['tags']:
    if t['name'] == '${ext_tag}':
        print(t['from']['name'])
        break
" 2>/dev/null) || true

            if [[ -n "${ext_pull}" ]]; then
                ext_sha="${ext_pull##*@sha256:}"
                ext_cache="${CACHE_DIR}/extensions-${ext_sha}"

                if [[ ! -f "${ext_cache}" ]]; then
                    tmpdir=$(mktemp -d)
                    if (cd "${tmpdir}" && oc image extract "${ext_pull}[-1]" \
                            --file="${EXTENSIONS_PATH}" 2>/dev/null); then
                        if [[ -f "${tmpdir}/extensions.json" ]]; then
                            mv "${tmpdir}/extensions.json" "${ext_cache}"
                        fi
                    else
                        echo -n "(ext extract FAIL for ${ext_tag}) "
                    fi
                    rm -rf "${tmpdir}"
                fi
            fi

            stream_list_json=$(echo "${stream_list_json}" | jq --arg s "${stream}" '. + [$s]')
        done

        # Create cross-arch hardlinks: other arches reference different image digests
        # for the same content, so we hardlink arch-specific filenames to the x86_64
        # cache files.  Hardlinks survive tarball round-trips without dangling refs.
        for arch in ${OTHER_ARCHES}; do
            arch_tag="${version}-${arch}"
            if ! grep -qFx "${arch_tag}" <<< "${ALL_TAGS}"; then
                continue
            fi
            arch_json=$(oc adm release info -o json "${REGISTRY}:${arch_tag}" 2>/dev/null) || {
                echo -n "(${arch}:FAIL) "
                continue
            }

            for stream in ${streams}; do
                ext_tag="${stream}-extensions"

                x86_sha=$(echo "${release_json}" | extract_digest "${stream}" 2>/dev/null) || true
                arch_sha=$(echo "${arch_json}" | extract_digest "${stream}" 2>/dev/null) || true
                if [[ -n "${x86_sha}" && -n "${arch_sha}" && "${x86_sha}" != "${arch_sha}" ]]; then
                    src="${CACHE_DIR}/sha256_${x86_sha}"
                    dst="${CACHE_DIR}/sha256_${arch_sha}"
                    if [[ -f "${src}" && ! -e "${dst}" ]]; then
                        ln "${src}" "${dst}"
                        symlink_count=$((symlink_count + 1))
                    fi
                fi

                x86_ext=$(echo "${release_json}" | extract_digest "${ext_tag}" 2>/dev/null) || true
                arch_ext=$(echo "${arch_json}" | extract_digest "${ext_tag}" 2>/dev/null) || true
                if [[ -n "${x86_ext}" && -n "${arch_ext}" && "${x86_ext}" != "${arch_ext}" ]]; then
                    src="${CACHE_DIR}/extensions-${x86_ext}"
                    dst="${CACHE_DIR}/extensions-${arch_ext}"
                    if [[ -f "${src}" && ! -e "${dst}" ]]; then
                        ln "${src}" "${dst}"
                        symlink_count=$((symlink_count + 1))
                    fi
                fi
            done
        done

        # Record in metadata
        jq --arg v "${version}" --argjson s "${stream_list_json}" \
            '.versions[$v] = {"streams": $s}' "${METADATA}" > "${METADATA}.tmp"
        mv "${METADATA}.tmp" "${METADATA}"

        new_count=$((new_count + 1))
        echo "OK"
    done
done

# Update timestamp
jq --arg t "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '.generated = $t' "${METADATA}" > "${METADATA}.tmp"
mv "${METADATA}.tmp" "${METADATA}"

# Count unique cache files
sha_count=$(find "${CACHE_DIR}" -name 'sha256_*' | wc -l)
ext_count=$(find "${CACHE_DIR}" -name 'extensions-*' | wc -l)

echo ""
echo "════════════════════════════════════════"
echo "Cache generation complete"
echo "  New versions:      ${new_count}"
echo "  Already cached:   ${cached_count}"
echo "  Errors:            ${error_count}"
echo "  Unique sha256_* files:     ${sha_count}"
echo "  Unique extensions-* files: ${ext_count}"
echo "  Cross-arch hardlinks:      ${symlink_count}"
echo ""

# Build tarball
echo "Building ${TARBALL}..."
tar -C "${CACHE_DIR}" -cf - . | zstd -o "${TARBALL}"
tarball_size=$(du -h "${TARBALL}" | cut -f1)
echo "  Tarball size: ${tarball_size}"
echo ""
echo "To pre-populate the controller cache:"
echo "  mkdir -p /tmp/rpmdb && tar --zstd -xf rpmdb-cache/rpmdb-cache.tar.zst -C /tmp/rpmdb/"
