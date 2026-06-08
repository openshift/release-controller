#!/usr/bin/env bash
#
# rpmdb-cache-backfill-arches.sh — retrofit cross-arch hardlinks into an
# existing rpmdb cache populated by rpmdb-cache-generate.sh.
#
# Run this once after upgrading rpmdb-cache-generate.sh to add cross-arch
# support.  Future runs of rpmdb-cache-generate.sh will handle new releases
# automatically.
#
# For each version recorded in metadata.json the script fetches the release
# manifest for aarch64, s390x, and ppc64le (JSON only — no image pulls),
# extracts the coreos and extensions image digests, and hardlinks them to the
# existing x86_64 cache files.
#
# Requirements: oc, skopeo, jq, python3

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE_DIR="${REPO_ROOT}/rpmdb-cache/data"
METADATA="${REPO_ROOT}/rpmdb-cache/metadata.json"
TARBALL="${REPO_ROOT}/rpmdb-cache/rpmdb-cache.tar.gz"
REGISTRY="quay.io/openshift-release-dev/ocp-release"
OTHER_ARCHES="aarch64 s390x ppc64le"

for cmd in oc skopeo jq python3; do
    if ! command -v "${cmd}" &>/dev/null; then
        echo "ERROR: ${cmd} is required but not found in PATH" >&2
        exit 1
    fi
done

if [[ ! -f "${METADATA}" ]]; then
    echo "ERROR: ${METADATA} not found — run rpmdb-cache-generate.sh first" >&2
    exit 1
fi

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

echo "Fetching tag list from ${REGISTRY}..."
ALL_TAGS=$(skopeo list-tags "docker://${REGISTRY}" 2>/dev/null | jq -r '.Tags[]')

versions=$(jq -r '.versions | keys[]' "${METADATA}" | sort -V)
version_count=$(echo "${versions}" | grep -c . || true)
echo "Found ${version_count} cached versions in metadata.json"
echo ""

sym_count=0
skip_count=0
err_count=0

for version in ${versions}; do
    # Streams were recorded per-version when the cache was first populated.
    mapfile -t streams < <(jq -r ".versions[\"${version}\"].streams[]?" "${METADATA}" 2>/dev/null)

    if [[ ${#streams[@]} -eq 0 ]]; then
        skip_count=$((skip_count + 1))
        continue
    fi

    # Fetch x86_64 release JSON to get the reference digests we cached against.
    x86_tag="${version}-x86_64"
    x86_json=$(oc adm release info -o json "${REGISTRY}:${x86_tag}" 2>/dev/null) || {
        echo "  ${version}: FAILED (x86_64 release info)"
        err_count=$((err_count + 1))
        continue
    }

    echo -n "  ${version}: "
    version_new=0

    for arch in ${OTHER_ARCHES}; do
        arch_tag="${version}-${arch}"
        if ! grep -qFx "${arch_tag}" <<< "${ALL_TAGS}"; then
            continue
        fi

        arch_json=$(oc adm release info -o json "${REGISTRY}:${arch_tag}" 2>/dev/null) || {
            echo -n "(${arch}:FAIL) "
            continue
        }

        for stream in "${streams[@]}"; do
            ext_tag="${stream}-extensions"

            x86_sha=$(echo "${x86_json}" | extract_digest "${stream}" 2>/dev/null) || true
            arch_sha=$(echo "${arch_json}" | extract_digest "${stream}" 2>/dev/null) || true
            if [[ -n "${x86_sha}" && -n "${arch_sha}" && "${x86_sha}" != "${arch_sha}" ]]; then
                src="${CACHE_DIR}/sha256_${x86_sha}"
                dst="${CACHE_DIR}/sha256_${arch_sha}"
                if [[ -f "${src}" && ! -e "${dst}" ]]; then
                    ln "${src}" "${dst}"
                    sym_count=$((sym_count + 1))
                    version_new=$((version_new + 1))
                fi
            fi

            x86_ext=$(echo "${x86_json}" | extract_digest "${ext_tag}" 2>/dev/null) || true
            arch_ext=$(echo "${arch_json}" | extract_digest "${ext_tag}" 2>/dev/null) || true
            if [[ -n "${x86_ext}" && -n "${arch_ext}" && "${x86_ext}" != "${arch_ext}" ]]; then
                src="${CACHE_DIR}/extensions-${x86_ext}"
                dst="${CACHE_DIR}/extensions-${arch_ext}"
                if [[ -f "${src}" && ! -e "${dst}" ]]; then
                    ln "${src}" "${dst}"
                    sym_count=$((sym_count + 1))
                    version_new=$((version_new + 1))
                fi
            fi
        done
    done

    echo "+${version_new} hardlinks"
done

echo ""
echo "════════════════════════════════════════"
echo "Backfill complete"
echo "  Versions processed: $((version_count - skip_count - err_count))"
echo "  Skipped (no streams): ${skip_count}"
echo "  Errors:               ${err_count}"
echo "  Hardlinks created:    ${sym_count}"
echo ""

if [[ ${sym_count} -gt 0 ]]; then
    echo "Rebuilding ${TARBALL}..."
    tar czf "${TARBALL}" -C "${CACHE_DIR}" .
    tarball_size=$(du -h "${TARBALL}" | cut -f1)
    echo "  Tarball size: ${tarball_size}"
    echo ""
    echo "To pre-populate the controller cache:"
    echo "  mkdir -p /tmp/rpmdb && tar xzf rpmdb-cache/rpmdb-cache.tar.gz -C /tmp/rpmdb/"
else
    echo "No new hardlinks needed — cache is already up to date."
fi
