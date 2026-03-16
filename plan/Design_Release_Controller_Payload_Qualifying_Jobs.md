# Design: Release Controller Payload Qualifying Jobs

Authors: Justin Pierce

Status: Draft

## **Overview**

Today, the OpenShift Release Controllers run two classes of CI testing jobs against release payloads: "Blocking" and "Informing". If all Blocking jobs for a release payload pass, the Release Controller will "Accept" the payload. If any of the Blocking jobs fail, the Release Controller will "Reject" the payload. Informing jobs, in contrast, are only informational and do not affect whether a payload is Accepted or Rejected.

When a release payload is Accepted, the Release Controller begins serving it as the latest payload available for the payload's stream. For example, if a payload X is Accepted in the "[4.19.0-0.nightly](https://amd64.ocp.releases.ci.openshift.org/#4.19.0-0.nightly)" Release Controller stream, when a client requests the latest 4.19 nightly from the Release Controller, it will be told about X. Payload acceptance, therefore, affects the behavior of systems beyond the Release Controller. A CI job which wants to test whether OpenShift can upgrade cleanly to a new 4.19 build of the platform Y, may install the latest 4.19 Accepted by the Release Controller X, and then trigger an upgrade to Y.

This has some subtle implications. The Release Controller creates new release payloads as pull requests (PRs) merge into the upstream OpenShift component repositories. These Pull Requests can merge constantly, regardless of the current state of payloads in the Release Controller. These PRs  may include regressions. The Technical Release Team (TRT) has tooling that is designed to help detect regressions after they merge, so that the flawed change can be reverted.  A string of Rejected payloads can impact TRT's ability to detect upgrade related regressions. Consider this sequence of events:

1. PR1 merges and introduces a serious regression into the platform. All release payloads containing PR1 begin to fail Blocking jobs.  
2. While PR1's regression remains unreverted, PR2..PRN merge.  
3. Several hours pass before PR1 is found and reverted.  
4. A payload X containing PR2..PRN is Accepted by the Release Controller.  
5. X begins to be used as an initial version for upgrade test jobs into subsequent payloads Y, Y2, ….  
6. A change introduced in PR2..PRN breaks upgrades from X to Y.  
7. The longer PR1 prevents a payload from being Accepted, the larger the potential number of PRs in the list PR2..PRN.  
8. Because TRT has no upgrade testing for an Accepted payload containing subsets of PR2..PRN, the team has a larger list of possible causes for the upgrade test failures.  
9. Upgrade failures can cause additional payloads to be Rejected – creating a stacked blindspot for detecting subsequent PRs merging and creating upgrade regressions while TRT determines the original culprit in PR2..PRN.

Payload rejection also inhibits other organizational workflows. For example, each sprint, the OpenShift engineering organization promotes an "Engineering Candidate" (EC) for stakeholders inside and outside of Red Hat to validate (e.g. an optional operator team may test their product on each new EC to ensure nothing has merged which will break their product's functionality well before that version of OpenShift goes GA). Rejected payloads are generally not considered viable for ECs  \- so a long string of Rejected payloads can jeopardize the EC publication & validation process.

Due to these impacts, Blocking jobs / Rejected payloads have both positive and negative consequences: 

* Positive: The engineering organization knows there is a problem and different practices ensure it cannot be ignored.  
* Negative: Long lasting strings of Rejected payloads interfere with our ability to analyze and assess our product.

Due to the "unignorable" consequence of Blocking jobs, many teams, both inside and outside the OpenShift engineering organization, want to make their test jobs Blocking. Consider a Hybrid Cloud Management (HCM) job which tests a complex managed service offering on top of OpenShift. It is in HCM's interest to make this a Blocking job because it ensures OpenShift Engineering knows quickly when a change is merged into the platform which interferes with HCM's service and OpenShift Engineering will be motivated to revert the change or, at least, work quickly with HCM to resolve the incompatibility before HCM discovers the issue themselves after OpenShift GA's a new version.

The negative consequences of Blocking jobs oppose the addition of new ones as they create wasteful work, delays, and risk for the OpenShift engineering organization when they don't work reliably. Consider the addition of 10 new Blocking jobs which work with 99% reliability with unimportant flakes[^1] impacting the remaining 1%. Without automatic retries, this means that payloads will be rejected approximately 10% of the time due to unimportant flakes. This becomes much worse as the reliability of the proposed Blocking jobs decreases. 10 jobs with 95% reliability means 40% of our payloads will be Rejected because of issues in the underlying testing.

The Release Controller can perform automated retries of jobs to reduce the impact of those with lower reliability \- allowing 3 retries of the 10 95% reliability jobs theorized above raises the overall reliability back to 99.9%. However retries have their own costs for the organization. They increase cloud costs and reduce the rate at which nightlies can be produced (leading to more PRs being tested in each new nightly and increased uncertainty in the cause of regressions when one is detected).

To account for these factors, proposed Blocking jobs must pass very reliably in the absence of a regression.

For jobs which do not meet the reliability criteria for Blocking, stakeholders are free to define Informing jobs. However, because there are no immediate[^2] consequences to Informing Jobs failing (i.e. it is the stakeholders job to monitor their results), failures are often left unaddressed for significant periods of time.

What is needed is a balance between Blocking and Informing \- a job run that will not necessarily interfere with test analysis or release mechanics, but one that is highly visible to the OpenShift Engineering organization and stakeholders when it fails.

This design document proposes "Qualifying Jobs" as this intermediate job class.

## **Goals**

1. It must be possible to fail a Qualifying job without preventing the Release Controller from accepting a payload.  
2. It must be possible for a failing Qualifying job to visually indicate that a payload is not qualified for a certain use without human interactions  
3. Qualifying jobs will allow notifications to be sent to stakeholders automatically when they are failing.  
4. Qualifying jobs will allow OpenShift Engineering to quickly communicate with stakeholders without interfering with payload acceptance.  
5. Qualifying jobs will help OpenShift Engineering and release management make more informed decisions when promoting ECs or named releases.  
6. The barrier to entry for teams to define qualifying jobs should be low.

## **Proposal**

### Configuration

The Release Controller configuration annotation (ex: [4.19 nightly](https://github.com/openshift/release/blob/4bd9c7d4efb2a96c1e9f26fde6facf15dc532aab/core-services/release-controller/_releases/release-ocp-4.19.json)) will be enhanced to support "qualifiers". Today, Release Controller configuration takes the form:

```
{
  ...other content ommitted...

  # A dictionary of jobs to run against payloads created in this
  # release stream.
  "verify": {

   # Each prowjob has a JobId unique to this configuration.
   # The underlying prowjob can change while the JobId
   # remains the same.
   "rosa-hcp": {
      // It true, the job is Informing. If false, the job
      // will be Blocking.
      "optional": true,
      // The prowjob name associated with this JobId.
      "prowJob": {
        "name": "periodic-ci-openshift-release-master-rosa-hcp"
      },

      // The number of times the Release Controller will retry
      // the prowjob before failing the JobId overall.
      "maxRetries": 2,
    },
    "another-job-id": {
      ...etc...
    }
  }
}
```

Qualifying jobs will be jobs (either Informing or Blocking, but usually Informing), which have one or more qualifiers associated with them. Qualifiers can be used in a number of different ways, but the primary use case for a qualifier is to indicate: "If all JobIds  associated with this qualifier succeed, the payload associated with the successful jobs is qualified for use (in my environment|for my service|with my product|by my team, etc)." In contrast, if even a single JobId associated with a qualifier fails, the payload is not qualified for use.

Qualifiers for a given JobId will be defined by a new "qualifiers" field in the Release Controller job configuration. Consider the following example:

```
"verify": {
   "rosa-hcp": {
      "optional": true,
      "prowJob": {
        "name": "periodic-ci-openshift-release-master-rosa-hcp"
      },

      "maxRetries": 2,

      // A dictionary of qualifiers and job specific configuration
      // values. Every qualifier has QualifierId.
      // A Job may have multiple qualifiers associated with it.
      // Most information related to a qualifiers is defined in 
      // a single configmap loaded by the Release Controller.
      "qualifiers": {
         // The rosa QualifierId is enabled for this job with
         // no job-specific configuration / overrides.
	  "rosa": {},
         // The "hcm" QualifierId is also enabled for this job.
         "hcm": {
           // job specific labels can be associated with qualifiers.
           // we can establish conventions around labels; here
           // ART is expected to block RC creation using a payload
           // failing this job.
           "failureLabels": [ "prevent_rc" ]
         }
}
    },
    "osd-aws": {
      ...normal job configuration...
      "qualifiers": {
	  "osd": {},
         "hcm": {}
}

    }
    "osd-gcp": {
      ...normal job configuration...
      "qualifiers": {
	  "osd": {},
         "hcm": {}
}
    }
}
```

Three Release Controller jobs are associated with this release stream:

1. "rosa-hcp" with qualifiers "rosa" and "hcm"  
2. "osd-aws" with qualifiers "osd" and "hcm"  
3. "osd-gcp" with qualifiers "osd" and "hcm"

When a job succeeds, the job earns the associated qualifier's **"badge"**. Once all qualifying jobs have run against a payload, the payload may, by virtue of underlying jobs, earn a qualifier's badge.

| rosa-hcp | osd-aws | osd-gcp | Overall Payload Badges Earned |
| :---- | :---- | :---- | :---- |
| Success | Failure | Failure | "rosa" |
| Failure | Success | Success | "osd" |
| Success | Success | Success | "hcm", "rosa", "osd" |

A single qualifier may be associated with multiple jobs involved in testing a given payload. From the perspective of badging, all jobs associated with a qualifier must pass in order for a payload to earn the badge.

The lion's share of configuration for each qualifier is stored in a configmap, loaded by the Release Controller separately from release stream configurations, containing JSON or YAML. 

```
qualifiers:
  hcm:    # A disabled qualifier is completely ignored.
    # This could be used for deprecation or temporary noise
    # reduction.
    enabled: true
    # The QualifierId has a badge name associated with it.
    # This is the display version of the qualifier's name.
    badgeName: "HCM"

    # A badge will always be displayed next to a job run in 
    # the release controller. It is optional for the badge to
    # be displayed at the payload level. Using "No" or "OnFailure"
    # may help keep main landing page uncluttered.
    payloadBadge: Yes | No | OnSuccess | OnFailure
    # Description will be displayed when hovering over the
    # badge.
    summary: "Hybrid Cloud Management"

    description: "A longer description when displaying badge details"

  rosa:
    badgeName: "ROSA"
    ...additional details omitted...

  osd:
    badgeName: "OSD"
    ...additional details omitted...

  hcm-pre-release:
    badgeName: "HCMEXP"
    ...additional details ommitted...    

  ...etc...
```

In the Release Controller user interface, badges appear next to jobs on the payload details page and, optionally, payloads on the main landing page.

![][image1]

### Failure Notifications

In addition to simple visual indications, qualifiers can also alert teams based on reliability requirements for underlying jobs not being met. 

```
qualifiers:
  hcm:    badgeName: "HCM"

    ...other details ommitted...
    # If a Qualifying job fails all retries, it can notify stakeholders
    # in a variety of ways. 
    failureNotifications:
      slack:
        escalations:
          - name: "monitor"  # Recognizable name for clear logging
            # Message will not be sent more often than the specified
            # number of hours.
            period: 24
            minFailures: 2
            channel: #forum-...
            mentions:
            - "@ocm-support"
            - "@patch-manager"
          - name: "high"
            period: 72
            minFailures: 8
            channel: #forum-...
            mentions: 
            - "@hcm-escalation-manager"
      

      # The Release Controller will open one Jira issue per
      # stream, job, and qualification id. The summary will include
      # key like "[<stream>:<JobId>:<QID>]". If a Jira issue including
      # the key is already open, another issue will not be opened.
      jira:
        project: OCPBUGS
        component: foo
        assignee: ...
        summary: ...
        description: ...
        # Sequential failures >= specified value will change the 
        # ticket priority.
        escalations:
        - name:  low
          failures: 2
          priority: low
        - name: normal
          failures: 4
          priority: normal
        - name: high
          failures: 6
          priority: high
          mentions:       
          - scuppett
        - name: critical
          failures: 8
          priority: critical
```

Reliability thresholds are tracked per Release Controller **job** in a given stream – not by badges at the payload level. For example, in our previous job definitions, the "osd" badge is associated with two jobs: "osd-aws" and "osd-gcp".  If an osd failure notification was configured as "failures: 2", if both jobs failed once, it is not sufficient for the notification to fire. At least one of the jobs would need to fail twice in a row. 

**Jira notifications**:

* Opened Jira tickets are never closed programmatically – even when the conditions under which they are opened abate. This is to ensure that stakeholders fully review failures and close out the ticket (vs statistical noise frequently opening and closing Jira tickets for qualifications on the threshold of notifications).  
* Priority is never reduced on open Jira tickets.  
* When conditions abate, a comment will be left on an open Jira ticket.  
* If conditions are satisfied again after abating, a new comment will be made on the Jira ticket.

**Notification Escalations**:

The Release Controller will track the number of failures of qualifying jobs per stream. As escalation criteria are met, Jira tickets priorities can be increased and new Jira/Slack notifications can be sent to increase visibility.

Jira Escalation Configuration Options

```json
# If >=2 of the last 10 runs failed.overLastRuns: 10failures: 2

# If <60% of the last 10 runs passed.overLastRuns: 10passPercentage: 60

# If the last 3 runs failed in a row.failures: 3overLastRuns: 3    # If omitted, overLastRuns = failures

# Find the last 20 runs or runs from the last two days, whichever# provides more samples, and escalate if <80% have passed.overLastRuns: 20overPeriod: 2dpassPercentage: 80needsInfo:- scuppett

# Find the last 20 runs or runs from the last two days, whichever# provides more samples, and escalate >=10 have failed.overLastRuns: 20overPeriod: 2dfailures: 10
```

**Notification Threads:**

By default, all jobs associated with a qualification will target the same notification "thread." For example, if the "osd-aws" and "osd-gcp" jobs were meeting their failure thresholds, there would only be one "osd" specific Jira issue opened tracking the issue (at the highest priority level prescribed between the two based on their respective number of failures) and a maximum of one Slack notification per day. 

However, it will sometimes be useful to target different teams or Jira projects for jobs, even if they contribute to the same overall qualification (e.g. if one OSD team supported the AWS offering and another supported GCP).  In this case, job specific overrides for the failure notifications can be used:

```
    "osd-gcp": {
      "qualifiers": {
	  "osd": {
           "failureNotifications": {
           "jira": {
             "thread": "gcp"
           }
         }
       }
    }
```

A unique key for each thread will be calculated for notifications: 

* Jira: "\<stream\>-\<qualification\>-\<project\>-\<component\>-\<thread\>".   
* Slack: "\<stream\>-\<qualification\>-\<channel\>-\<thread\>"

If the same key is calculated for two different jobs, they will share the same failure notification.

### User Interface

Qualifications can optionally be displayed as "badges" alongside payloads. Badges will be compatible with existing team "Approval" and "Rejection" functionality exposed by the Release Controller (i.e. team approval / rejection triggered by "release-tool") and will appear as qualification badges but they cannot not have notifications associated with them.

![][image1]

Clicking on a badge in the Release Controller user interface will navigate the user to an up-to-date report for the badge in that stream.

The report should include success & failure metrics for jobs, links to recent notifications for impacted jobs (both active and resolved). For example, if a nightly payload has failed a "Telco" badge qualification, and it was configured to create a Slack notification for failures, the Release Controller UI should expose a link to open the Slack conversation(s) associated with the badge so that stakeholders interested in progress on an issue can review any action being taken.

Example report

---

`4.19.0-0.nightly`

`Qualification: Telco`

`Description: <description>`

`Overall: ![][image2]`

`Failure Labels:`

* `prevent_ec`  
* `prevent_rc`  
* `prevent_ga`

  `Job Overview:` 

* `![][image3]periodic-telco-job-1`  
       `Recent runs: (F F F) (F F F) (F F F) (S F) (S) (S) (S)`   
       `Active Notifications:`   
      ` ![][image4] Slack    ![][image5]Jira ticket TELCO-3304`   
      ` ![][image6]Notification History`  
* `![][image7]periodic-telco-job-2`  
* `![][image7]periodic-telco-job-3`

---

### Failure Labels

Within the Release Controller configuration, job qualifiers can be annotated with failure labels. When a job fails a qualifier, the labels are associated with the failure. The labels aggregate up to the payload level for failed qualifiers. 

Labels are simply strings from the release controller's perspective, but can eventually be used programmatically to instruct ART how to treat certain payloads. For example, a payload labeled with "prevent\_rc" by any failed qualifier, could be rejected by ART code if an attempt is made to promote the nightly as an RC release (but it would be permitted to be promoted as an EC release).

### API

As with team approvals today, qualifications will be able to be applied and queried by HTTP based requests to the Release Controllers. In addition to the existing approval API, the qualification API will be able to set and retrieve label information.

It must be possible to retrieve the same information from the API as shown on the example report above; including but not limited to failure labels, contributing jobs, Jira tickets, and links to the Slack thread.

Additionally, an API call should be available to retrieve the latest payload with a particular qualification, for example:

​​[https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestream/4.19.0-0.nightly/latest?qualified=Telco](https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestream/4.19.0-0.nightly/latest?qualified=Telco)

should return the most recent Accepted payload qualified for Telco.  It must be possible to chain multiple qualifiers together to get a payload qualified for both Telco and HCM for example.

[^1]: All flakes are bugs somewhere. Here, flakes are defined as issues that are either (a) outside of OpenShift Engineering's control (e.g. an outage on quay.io, a bug in a layered product or service, or unpredictable cloud provider issues) or (b) a bug whose pursuit, for any variety of reasons, falls outside the organization's current focus.

[^2]:  Informing jobs do contribute results into Component Readiness. So regressions can be detected through statistical analysis after sufficient time.

[image1]: <data:image/png;base64,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>

[image2]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAADQAAAAUCAYAAADC1B7dAAACsElEQVR4Xu2UT0iaYRzH7R9RlqxDs/YH3dTRPIYRo0skUWCXLl487RCjMdzY8Lg2GHQb7iDssM3tEIxO4YK8rUEkhEjDF5JCUd/MTB1Rl5pW3z3vo717e5/xOnbqlT3wkff9+fr1/fB7np8GdbY09PPwEBgeBlpaSEWjLrq6gEJBJtTbCzQ24mB+Hvtra6ri58gIoNUCe3tVof19nHV24kc2i2KxqEpOTCZgcLAqtLmJE6OReUhNlAcGKtvvv9BfYrfbYTAYGOTPachLyGv/Qm2hfB6xoWFwBjPBAs54Bzuf55mgWoRCIczOzl6oraysiNdSoVQqha2tLfE+l8shEokwmX+iplBiahrRmyYqIhDtNYAj90UyHuVhSkiFsmTo9PT0YGxsDHq9ntbOhebm5tDW1kaGlRYOhwPxeBytra3o7u6G1WplcuUoCu18+ES7wj/xIO97i/SDRzgrl6kg//IVE6aEVGh8fFys+/1+BAIBKhQOh6nM+XdCZzo6OsR7r9eLdDrNZEtRFIrfn6p0hkgdfv2GMql9v3KV1jYGhpgwJaRCwhlaXFwU2ST/KwgtLS3BbDZf+J10KwrdWlhYYLKlKApl3vsrHXroRv7dR/CPn+H06Ih2KP38BROmhFTI4/GI1xzHYX19nb749vY2dDodEokEkskkZmZm4HK5EAwG6Ta1WCxMrhxFoQLPgzPdxd7rN4hev0XPDu9+iug1Iwrk4MrDlJAKCS8udEI4R8KWisViYieWl5fR3NxMt57b7UYmk0F7ezs9U06nk8mVoyhE2d3Fhu1eZcIRojduIxv4wgRdFmoLqYz6E7LZgIaGqlCpBDQ14aDGJLnMnJGhgunpqpCwfD7aspO+PpRGR1UFmSbAxARwfCwREtbqKjA5CfT3qwsy5nF6Kmr8FqqT9QszfY52sRKjbwAAAABJRU5ErkJggg==>

[image3]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAsAAAALCAIAAAAmzuBxAAABDElEQVR4XmP4DwN/3r57Ulp52zvwfmzit4uX4eIMEOqmnfMlWeXLCmpQJK96SUIOoeKOTwBICEnFRTGZS1IKF4QkQCr+vHsHkQOZ5Oh2QUD8Ar8YkH0/MRUo+GH9BobnbZ0gfYrq1wzMgBK/X70Gko+Kyy/LqQDF73gHMDytqYeaLCrz9cJFoPS/nz+BJkEEb1g5MDxraYNar6L5/+/fvz9/AhXdcvaACN529WT4cecO0JmXZZWBEo/Lqy8Iil/TMwGyb1g7AFW8njEL5Jdr+iaXpBXvJ6SClIK1XjeyuKKpd1FUGurb///+XVHVhtoF97C47O8XL2EqwODtilV3/IJuWNrfcnADehAuDgDNn93iaMnSMAAAAABJRU5ErkJggg==>

[image4]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABQAAAAUCAYAAACNiR0NAAAAtElEQVR4XpXRzQ2DMAyGYSbpCBwZprt0KQ5sgFijxx56YANQIhmF1z8xlj4pOPajSAxDUNvvOKxwrlsEvHBPFReyoVOLQ09DzwWndb/O4/JR9ybIyxaTFEzCOYXywkLTIJstIik9gh6sQGLWC4mHoIVKv32dhbng9/WukbP0//N4C/dMUDCGmIcqUFDi5buHVdD70wQj5IY9BSP0AnuonNNYBGZDrxaHsqGjigteuNctAhnoBPkHNClkingnAAAAAElFTkSuQmCC>

[image5]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABQAAAAUCAYAAACNiR0NAAACwElEQVR4XsWS2U8TQRzHF9pu7yPGxEj60Bg0hhiDCYkhxie1nCKIgHKoqCiXSIyP/iu+qfFAFAmKUURELhVBg0gELK1ylKuUFmhLu19nJm1lt+CTid/kk5nf/L7z3Znd5bj/KQBxFOn6ltqxv0xrvOoXttX5YKoLCMZrAToy6HpkHqkNtQFYknNN0hwmszlVbaxeg6HWJ5hqSCCBzKPQ3lYkJSXx0jzOULUEfZUbhko3GyNzVle6oK12R9EQr46ssX54nyiMvhddxQz0FXObknZzDIIgQCrTlQUYLi9Cf2kmNlB7YQq6i5Oboi0ZwujEL4RCIUZExrPjYc80Lf98LFpoyn9CW26Poj5ngz8UxPyaByPOdfSOLKOx0wF9Xks0UFc6wrya8w5xIJWm7Ac05IkRdEUD5JpBttHlDWB00o3u4QUk17RHA5XFX6N+USAt1KXfIKJ4EHxWM+ac83C5l7Dk8bHQ1PoOTE/NsUBV4WeoSoYJQ7GBqtOkSQiGfFjxhWCf9WPQ5sXrAScevR1np11e9eNg/RuoctugyLwHvrAH6jNf2L6YQL7gI/iC95iYcLAX710LwuH0oP/7LF702REMBtipDl3vIr5PUBIv9SsL+8EXfYgNVOR3gz/VA2X2U6iO3sLYuA3zLi/s0x70Dc1Abn1AQoM4fKML8vxeKBjdUUSBVPzJd1DkdRI6wOe8hCL7GZY9K/CsrmNschHyE22QWe9AkdUY9omJOaE8px2ynFeIJ9CR1qbMu+yai+5VFriRiI95j7fSv158QnlWiyDLakWU7OeQpTeid9gGH/kXWb2xL/GKwpiMu3fFZzQjLuOJmGP3wR25jfjMptheGF6/Z680jkmx80AKl/5Y4NIaKKBjnLUBFC6tSYrAWR+C274vJea6G0WbiYmJyoSEBI3ZbFZHsFgsKinU+9ewf6HflYPCoDtj2GwAAAAASUVORK5CYII=>

[image6]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAwAAAALCAYAAABLcGxfAAAAO0lEQVR4XmP48OHjf1IwA7oACG+pcf5vU7MdQxynhixVzf+KqgUY4nTWoAhWhBtjaCDZBvpqwIdJ1gAAJknISOq8iE0AAAAASUVORK5CYII=>

[image7]: <data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAwAAAALCAIAAADEEvsIAAABBElEQVR4XmP4DwPvvn9cf2uv56rs2M1VN98+gIsDAQOEUp7uIzHJSXqKKwRJTXYRneiw9Oo2hCKFaZ4yMGk4kpnixtVjfvnVbZCijz+/SExyRlPB32dtND8SJN2uDyKzd7UBRQX6bZBV3AK7SWm6t8Rk599//zC4rcyUnuIWtqFUaooLRJHZwmigiosvbwJ1Sk52+fLrG0PMpkqgcqBo7OZqoGMhKrxWZUPMBkr9/POL4eHHZ0C/iE1y/A8OBYgKuNVApVDfAQ0A+k5kosPMC2uRVQDR3EvroYqAQGIyKJCARsKlJSc7p2xrgMhCFQHBvEsb8nd3At1kvzSp5+TC2+8ewaUA2SzmLMFk7gQAAAAASUVORK5CYII=>