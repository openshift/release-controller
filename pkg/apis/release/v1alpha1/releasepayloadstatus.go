package v1alpha1

func (in *ReleasePayloadStatus) GetJobs() []JobStatus {
	var jobs []JobStatus
	jobs = append(jobs, in.BlockingJobResults...)
	jobs = append(jobs, in.InformingJobResults...)
	return jobs
}
