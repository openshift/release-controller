# Static rpmdb Cache

This directory contains a pre-populated cache of rpmdb and extensions metadata for OCP releases, used to eliminate cold-start penalties when the release-controller restarts.

## Contents

- `rpmdb-cache.tar.gz` — Compressed tarball of cached data files
- `metadata.json` — Tracking file recording which versions have been cached
- `data/` — Working directory where cache files are populated

## Cache Files

The tarball contains two types of JSON files:

- `sha256_<digest>` — RPM package list from `oc adm release info --rpmdb`, keyed by the coreos image digest. Multiple z-streams can share the same coreos image, so deduplication happens automatically.
- `extensions-<digest>` — Extensions metadata extracted from the `*-extensions` image, keyed by the extensions image digest.

Each file is ~25–30 KB uncompressed; the compressed tarball is typically 5–20 MB depending on the version range covered.

## Usage

Extract the cache on controller startup:

```bash
mkdir -p /tmp/rpmdb && tar xzf rpmdb-cache/rpmdb-cache.tar.gz -C /tmp/rpmdb/
# Then start the release-controller
```

This can be done in a Kubernetes init container:

```yaml
initContainers:
- name: populate-rpmdb-cache
  image: busybox:latest
  volumeMounts:
  - name: rpmdb-cache-volume
    mountPath: /cache
  - name: tmp-rpmdb
    mountPath: /tmp/rpmdb
  command:
  - sh
  - -c
  - |
    mkdir -p /tmp/rpmdb
    tar xzf /cache/rpmdb-cache.tar.gz -C /tmp/rpmdb/
```

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

- **Architecture**: x86_64 only
- **Versions covered**: Typically 4.12.0 through current (configurable in the script)
- **Stream discovery**: The script discovers machine-OS streams by finding `*coreos*-extensions` tags paired with their base tags in each release's component manifest.
- **4.12 special case**: Uses `rhel-coreos-8` / `rhel-coreos-8-extensions` instead of `rhel-coreos`
- **4.21+**: Dual streams `rhel-coreos` + `rhel-coreos-10`

## Why This Helps

On a fresh restart, the release-controller processes hundreds of changelog pages simultaneously. Each page triggers cold `oc adm release info --rpmdb` calls that would normally take 4–8 seconds per release with disk cache misses, monopolizing the 16-slot `RpmdbOCSlots` semaphore. This causes most requests to get 429 responses while waiting for the semaphore.

With a pre-populated cache, these files are served from `/tmp/rpmdb/` instantly, reducing semaphore hold time from ~30 seconds to ~6–12 seconds and allowing 3–5x more concurrent changelog requests.
