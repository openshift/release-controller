# Static rpmdb Cache

This directory contains a pre-populated cache of rpmdb and extensions metadata for OCP releases, used to eliminate cold-start penalties when the release-controller restarts.

## Contents

- `rpmdb-cache.tar.zst` — Compressed tarball of cached data files
- `metadata.json` — Tracking file recording which versions have been cached
- `data/` — Working directory where cache files are populated

## Cache Files

The tarball contains two types of JSON files, keyed by image digest:

- `sha256_<digest>` — RPM package list from `oc adm release info --rpmdb`, keyed by the coreos image digest. Multiple z-streams can share the same coreos image, so deduplication happens automatically.
- `extensions-<digest>` — Extensions metadata extracted from the `*-extensions` image, keyed by the extensions image digest.

Each file is ~25–30 KB uncompressed; the compressed tarball is typically 5–20 MB depending on the version range covered.

### Cross-architecture coverage

Different architectures (x86_64, aarch64, s390x, ppc64le) use different image digests for the same RHEL CoreOS content. The generation script fetches the release manifest for each additional arch and creates hardlinks so that arch-specific digest lookups hit the cache without pulling those images. Hardlinks preserve correctness across tarball round-trips (no dangling references).

## Usage

The archive is baked into the release-controller image at
`/usr/share/release-controller/rpmdb-cache.tar.zst`. A Kubernetes init
container extracts it into a shared `emptyDir` volume before the controller
starts. Add the following to the release-controller Deployment/StatefulSet:

```yaml
initContainers:
- name: populate-rpmdb-cache
  image: release-controller:latest   # same image as the main container
  command:
  - /bin/bash
  - -c
  - |
    mkdir -p /tmp/rpmdb
    tar --zstd -xf /usr/share/release-controller/rpmdb-cache.tar.zst -C /tmp/rpmdb/
  volumeMounts:
  - name: rpmdb-cache
    mountPath: /tmp/rpmdb

# In the main release-controller container, add:
#   volumeMounts:
#   - name: rpmdb-cache
#     mountPath: /tmp/rpmdb

# In spec.volumes, add:
#   - name: rpmdb-cache
#     emptyDir: {}
```

The init container uses the same image, so no separate image is needed.
`--zstd` is supported natively by GNU tar 1.31+ (RHEL 9 ships 1.34).

## Generating / Updating the Cache

Use `hack/rpmdb-cache-generate.sh` to generate or update the cache:

```bash
# Generate cache for specific minor versions
hack/rpmdb-cache-generate.sh 4.21 4.22

# Generate cache for all versions (4.12 through 4.22)
hack/rpmdb-cache-generate.sh

# Re-running the script only processes new versions (idempotent)
hack/rpmdb-cache-generate.sh 4.21 4.22
```

**Note:** Processing all ~500 GA z-stream releases is time-consuming (several hours with network latency). It's recommended to:
1. Generate incrementally for recent minor versions (4.20+) first
2. Periodically add new z-streams as they're released
3. For older versions (4.12–4.19), run the script once and commit the cache

The script reads `metadata.json` to skip versions already cached, so re-runs only fetch new releases.

## Implementation Details

- **Architectures**: x86_64 (primary); aarch64, s390x, ppc64le covered via hardlinks
- **Versions covered**: Typically 4.12.0 through current (configurable in the script)
- **Stream discovery**: The script discovers machine-OS streams by finding `*coreos*-extensions` tags paired with their base tags in each release's component manifest.
- **4.12 special case**: Uses `rhel-coreos-8` / `rhel-coreos-8-extensions` instead of `rhel-coreos`
- **4.21+**: Dual streams `rhel-coreos` + `rhel-coreos-10`

## Why This Helps

On a fresh restart, the release-controller processes hundreds of changelog pages simultaneously. Each page triggers cold `oc adm release info --rpmdb` calls that would normally take 4–8 seconds per release with disk cache misses, monopolizing the 16-slot `RpmdbOCSlots` semaphore. This causes most requests to get 429 responses while waiting for the semaphore.

With a pre-populated cache, these files are served from `/tmp/rpmdb/` instantly, reducing semaphore hold time from ~30 seconds to ~6–12 seconds and allowing 3–5x more concurrent changelog requests.
