source code (product or tools)
  * history rewrites
  * malicious commits
  * PR jobs

image builders
  * compromised environment
  * malicious images

release accumulators
  * compromised environment
  * compromised input images
    * injection
    * replay

publishers
  * compromised environment
  * compromised tool images
  * incorrect release input

verifiers
  * compromised environment
  * compromised tool images

signers
  * compromised environment
  * compromised tool images
  * malicious verifiers

clusters
  * compromised environment
  * compromised release images


Separate CI and release enviroments into zones

  * CI zone verifies source
    * Observers verify integrity and audit
    * Compromise of CI only affects outputs, not process
  * Process zone verifies github process
    * Compromise only affects integrity of CI process
    * Prow jobs, release controller
  * Release zone takes verified source and builds verified images
    * Release zone needs to be on a time and process delay from CI zone
    * Source sync to dist-git, OSBS launch, sync to quay, publish artifacts
    * Creates a decision artifact (release image stream snapshot) that can reproduce release
  * Release CI zone takes verified images and creates releases
    * Invoke untrusted entities to verify results
    * Once complete, sign (on a delay)
  * Auditor zone(s) monitor all other processes and check their output