#!/usr/bin/env python3
"""Read-only query for the newest ReleasePayloads with empty coordinates.

Newest means metadata.creationTimestamp, not the release version or build date.
Usage: python3 hack/newest-empty-release-coordinates.py [--context app.ci] [--limit 5]
Requires oc, Python 3, and read access to ReleasePayloads across all namespaces.
"""

import argparse
from datetime import datetime, timezone
import json
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", default="app.ci", help="oc context (default: app.ci)")
    parser.add_argument("--limit", type=int, default=5, help="number of results (default: 5)")
    args = parser.parse_args()
    if args.limit < 1:
        parser.error("--limit must be positive")
    result = subprocess.run(
        [
            "oc", f"--context={args.context}", "get",
            "releasepayloads.release.openshift.io", "--all-namespaces",
            "-o", "json", "--request-timeout=60s",
        ],
        check=True, stdout=subprocess.PIPE, text=True,
    )
    data = json.loads(result.stdout)
    # This matches the controller's len(spec.releaseCoordinates) == 0 check.
    empty = [p for p in data["items"] if not p.get("spec", {}).get("releaseCoordinates")]
    latest = sorted(
        empty, key=lambda p: p["metadata"].get("creationTimestamp", ""), reverse=True,
    )[:args.limit]
    print(json.dumps({
        "context": args.context,
        "queriedAtUTC": datetime.now(timezone.utc).isoformat(),
        "newestWithoutReleaseCoordinates": [
            {
                "namespace": p["metadata"]["namespace"],
                "name": p["metadata"]["name"],
                "creationTimestamp": p["metadata"].get("creationTimestamp"),
                "payloadType": p.get("spec", {}).get("payloadType"),
                "payloadCoordinates": p.get("spec", {}).get("payloadCoordinates"),
            }
            for p in latest
        ],
    }, indent=2))


if __name__ == "__main__":
    main()
