# Konflux Release Setup

This document explains how to configure the release controller to start a Konflux release
pipeline when a release tag is accepted.

The `konfluxRelease` publish step creates an `appstudio.redhat.com/v1alpha1` `Release` on a
Konflux cluster, referencing an existing Snapshot and a ReleasePlan. Konflux then runs the
managed release pipeline for that ReleasePlan.

## How the Snapshot is resolved

A Konflux `Release` refers to a **Snapshot by name**, and the release controller cannot work
that name out for itself:

- Konflux builds the image first, and the integration service creates the Snapshot afterwards
  from that image plus the latest builds of its sibling components. The build cannot record a
  Snapshot name that does not exist yet, so the image carries no Snapshot label.
- The release controller deliberately does not query Konflux to search for one.

Instead, **whatever promotes the image into the release image stream must stamp the Snapshot
name onto the image stream tag**:

```
release.openshift.io/konflux-snapshot: <snapshot-name>
```

The promoter runs after the Snapshot exists, so it knows the name. If the annotation is
missing, the publish step records a `KonfluxSnapshotMissing` warning event against the image
stream and does nothing; it will pick the tag up automatically if the annotation is added
later, as long as the tag is still the newest accepted one.

Because this annotation has to be applied by something outside the release controller, the
step is only supported on streams whose tags are created externally, which means
`"as": "Layered"` or `"as": "Stable"`.

## Step 1: Create the kubeconfig secret

The controller does not hold Konflux credentials itself. Each publish step names a secret in
the release controller's **job namespace**, which is mounted into the job that creates the
Release. This is what allows different streams to target different Konflux clusters or
tenants.

```bash
kubectl create secret generic konflux-kubeconfig \
  --from-file=kubeconfig=./kubeconfig \
  --namespace=your-release-controller-job-namespace
```

The secret must have a `kubeconfig` key.

## Step 2: Grant permissions on the Konflux cluster

The identity in that kubeconfig needs only the following in the tenant namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: release-controller-releaser
  namespace: ocp-hypershift-tenant
rules:
  - apiGroups: ["appstudio.redhat.com"]
    resources: ["releases"]
    verbs: ["get", "create"]
```

`get` is required as well as `create`: the job checks whether the Release already exists
before creating it, so that a retry can never start a second release pipeline.

## Step 3: Configure the publish step

```json
{
  "name": "hypershift-ci",
  "as": "Layered",
  "publish": {
    "konflux-prod": {
      "konfluxRelease": {
        "namespace": "ocp-hypershift-tenant",
        "releasePlan": "hypershift-prod-release-plan",
        "secretName": "konflux-kubeconfig",
        "cliImage": "quay.io/openshift/cli:latest",
        "data": {
          "mapping": {
            "components": [
              {
                "name": "hypershift-operator-main",
                "repositories": [
                  {"url": "quay.io/redhat-services-prod/crt-redhat-acm-tenant/hypershift/hypershift-operator"}
                ],
                "componentTags": ["${RELEASE_TAG}"]
              }
            ]
          }
        }
      }
    }
  }
}
```

| Field | Required | Description |
| --- | --- | --- |
| `namespace` | yes | The Konflux tenant namespace holding the ReleasePlan and Snapshot. The Release is created here. |
| `releasePlan` | yes | The ReleasePlan to invoke. |
| `secretName` | yes | The secret in the job namespace holding the `kubeconfig`. |
| `cliImage` | yes | An image containing `oc`. Required because layered streams have no CLI image of their own. |
| `data` | no | Passed through as the Release's `spec.data`, which Konflux merges with the ReleasePlan's own data. |

The publish step name (`konflux-prod` above) must be a valid DNS1123 label, because it forms
part of an annotation key.

## Tagging the released image with the release tag

Konflux takes the tags it pushes from `spec.data.mapping`, so the release tag has to be
substituted into the data.

The `data` of the Release is merged with that of the ReleasePlan and the
ReleasePlanAdmission, and `mapping.components` entries are combined by `name`, so the `name`
must match the component as the ReleasePlanAdmission names it. `repositories` is required.

The tags a component is pushed under are the union of three keys, so `${RELEASE_TAG}` is
added to whatever the ReleasePlanAdmission already asks for rather than replacing it:

| Key | Scope |
| --- | --- |
| `mapping.defaults.tags` | Every repository of every component. |
| `mapping.components[].componentTags` | Every repository of one component. |
| `mapping.components[].repositories[].tags` | One repository. |

Note that the component level key is `componentTags`, not `tags`. A `tags` key written
directly on a component is read by nothing and is silently ignored, so the release is
published without it.

Prefer `componentTags` over `repositories[].tags` for the release tag. Repository entries are
combined across the merged sources as whole objects rather than by `url`, so adding a `tags`
key to a repository that the ReleasePlanAdmission also declares yields two entries for the
same URL instead of one.

String values in `data` may reference:

| Variable | Expands to |
| --- | --- |
| `${RELEASE_TAG}` | The name of the accepted release tag, e.g. `4.21.0-0.nightly-2026-01-02-030405`. |
| `${RELEASE_NAME}` | The name of the release stream, e.g. `hypershift-ci`. |
| `${SNAPSHOT}` | The name of the Snapshot being released. |

These are expanded by the release controller before the Release is created. They use `${...}`
rather than the `{{ ... }}` that Konflux itself uses for mapping tags, so the two cannot be
confused: `{{ git_sha }}` and friends are passed through untouched for Konflux to expand at
release time, and both forms can appear in the same tag list.

A reference to an unknown variable is rejected when the config is validated, so a typo such as
`${RELEASE-TAG}` fails up front rather than being pushed as a literal image tag.

Note that `data` counts against the 16KiB limit on the serialized release config.

## Exactly-once behaviour

Unlike the other publish steps, creating a Konflux Release is not idempotent: doing it twice
releases twice. Three things prevent that:

1. Once the Release has been created, the tag is annotated with
   `release.openshift.io/konflux-release-<step-name>`, holding the name of the Release. This is
   the authoritative guard, and it survives job garbage collection and controller restarts.
2. The job itself covers the window between its creation and the tag being annotated.
3. The Release is given a deterministic name derived from the tag, the step and the Snapshot,
   and the job treats an already existing Release as success.

## Recovering from a failed job

A failed job is deliberately left in place, which blocks the step from running again, so a
Release is never created twice by a retry loop. A `KonfluxReleaseFailed` warning event is
recorded against the image stream.

Inspect the logs, then either:

```bash
# Retry: the job is recreated on the next sync.
oc delete job -n <job-namespace> <job-name>
```

or, to suppress the step for a tag that was already released by hand:

```bash
oc annotate istag <stream>:<tag> release.openshift.io/konflux-release-<step-name>=<release-name>
```
