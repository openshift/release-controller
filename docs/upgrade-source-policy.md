# Target-scoped previous-minor sources

Verification jobs and release periodics can opt into `previousMinorOverride` when
`upgrade` is true and `upgradeFrom` is `PreviousMinor`. This policy overrides the
source only for the configured target major.minor; other targets retain existing
`PreviousMinor` behavior. It cannot be combined with `upgradeFromRelease`.

For example, a stable OKD stream can verify every 5.0.x payload from an accepted
4.22 source without changing how 5.1+ finds its previous minor:

```json
"upgrade-minor": {
  "upgrade": true,
  "upgradeFrom": "PreviousMinor",
  "previousMinorOverride": {
    "targetVersion": "5.0",
    "stream": "4-scos-stable",
    "version": "4.22"
  },
  "prowJob": {
    "name": "release-openshift-okd-scos-installer-e2e-aws-upgrade-from-scos-stable"
  },
  "maxRetries": 2
}
```

`targetVersion` and `version` must contain exactly a major.minor. Matching ignores
patch and prerelease identifiers, so the example applies to `5.0.0-okd-scos.0`,
`5.0.0-okd-scos.1`, and `5.0.1-okd-scos.0`, as well as 5.0 prereleases.

`stream` matches the exact release configuration name, not a constructed candidate
name. The controller selects the highest semantic version with the requested
source major.minor that is Accepted in a Stable stream in the target's namespace.
ReleasePayload conditions take precedence over legacy phase annotations. As with
ordinary stable selection, accepted prerelease tags are eligible. A named stream
in another architecture namespace cannot supply a source.

A missing stream, missing eligible source, or unavailable pull spec produces an
error. The controller retries resolution rather than recording a successful no-op
upgrade check. A blocking verification therefore cannot accept a matching target
without a source. `optional`, `async`, and other job settings retain their existing
meaning; the example is blocking because `optional` is omitted.

Deploy controller support before enabling this field in release configuration.
The controller extension alone does not activate any new verification jobs or
change streams that omit the policy. Architecture-specific streams must opt in
with their own source stream and compatible Prow job.
