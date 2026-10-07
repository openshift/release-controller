#!/usr/bin/env python3
"""Read-only count of ReleasePayloads with absent, null, or empty coordinates.

Usage: python3 hack/count-empty-release-coordinates.py [--context app.ci]
Requires oc, Python 3, and read access to ReleasePayloads across all namespaces.
"""

import argparse
from collections import Counter
from datetime import datetime, timezone
import json
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", default="app.ci", help="oc context (default: app.ci)")
    args = parser.parse_args()
    result = subprocess.run(
        [
            "oc", f"--context={args.context}", "get",
            "releasepayloads.release.openshift.io", "--all-namespaces",
            "-o", "json", "--request-timeout=60s",
        ],
        check=True, stdout=subprocess.PIPE, text=True,
    )
    data = json.loads(result.stdout)
    items = data["items"]
    # This matches the controller's len(spec.releaseCoordinates) == 0 check.
    empty = [p for p in items if not p.get("spec", {}).get("releaseCoordinates")]
    field_states = Counter()
    for payload in empty:
        spec = payload.get("spec", {})
        if "releaseCoordinates" not in spec:
            field_states["absent"] += 1
        elif spec["releaseCoordinates"] is None:
            field_states["null"] += 1
        else:
            field_states["emptyArray"] += 1
    print(json.dumps({
        "context": args.context,
        "queriedAtUTC": datetime.now(timezone.utc).isoformat(),
        "resourceVersion": data.get("metadata", {}).get("resourceVersion"),
        "totalReleasePayloads": len(items),
        "emptyReleaseCoordinates": len(empty),
        "emptyByNamespace": dict(sorted(Counter(
            p["metadata"]["namespace"] for p in empty
        ).items())),
        "emptyByPayloadType": dict(sorted(Counter(
            p.get("spec", {}).get("payloadType", "<unset>") for p in empty
        ).items())),
        "emptyByFieldState": dict(field_states),
    }, indent=2))


if __name__ == "__main__":
    main()
