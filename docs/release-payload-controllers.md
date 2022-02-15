
# release-payload-controller
The `release-payload-controller` is a single binary that contains all the controllers that are responsible for handling all the interactions with `ReleasePayload` objects.

## release-controller
The `release-controller` is responsible for creating the following:
1. The `batch/v1` Jobs responsible for creating the physical releases
2. The `ReleasePayload` object
3. The Release Verification `ProwJobs`

## release-creation-job-controller
The `release-creation-job-controller` watches for `batch/v1` Jobs in the `jobs-namespace` for jobs annotated with: `release.openshift.io/releaseTag`.
```
writes: TODO: Add new JobStatus for the "release creation job"
reads: batch/v1 Jobs created by the release-controller to generate actual releases
```
### pseudocode
```
if job
else, PayloadCreated=false with status of the job
```

## release-payload-created-controller
```
writes: .status.condition.type=PayloadCreated
reads: batch/v1 Jobs created by the release-controller to generate actual releases
```
### pseudocode
```
if job is successful, then PayloadCreated=true
else, PayloadCreated=false with status of the job
if no job is found and the current payloadcreated state is not true or false, this is unknown, right/
```

## release-payload-accepted-controller
```
writes: .status.condition.type=PayloadAccepted
reads: status.jobstatus, spec.payloadOverride.override
```
### pseudocode
```
if PayloadRejected==true, the payloadaccepted must be false.
if (spec.payloadOverride.override == "Accepted"), then PayloadAccepted=true
if status.blockingJobResults.state == "Success", then PayloadAccepted=true
If there are no blockingjobResults, then the condition is false. This means that the thing creating the list of blocking jobs needs to create that list all at once.
else, PayloadAccepted=false with a list of jobs that need to pass as a reason
```

## release-payload-rejected-controller
```
writes: .status.condition.type=PayloadRejected
reads: status.jobstatus, spec.payloadOverride.override
```
### pseudocode
```
if PayloadAccepted==true, the payloadrejected must be false.
if (spec.payloadOverride.override == "Rejected"), then PayloadRejected=true
if status.blockingJobResults.state == "Failed", then PayloadRejected=true
else, PayloadRejected=false with a list of jobs that need to pass as a reason
```

## release-payload-failed-controller
```
writes: .status.condition.type=PayloadFailed
reads: TODO: Add new JobStatus for the "release creation job"
```
### pseudocode
```
if status."release creation job".state == "Failed", then PayloadFailed=true with error why?
else, PayloadFaild=false
```

## release-payload-prowjob-controller
```
writes: update to status.jobstatus.results
reads: status.jobstatus, prowjobs*
```
### pseudocode
```
for each status.jobstatus,
if state is terminal, do nothing
else
locate the prowjobs
if present, pull status
if missing, mark unknown
```