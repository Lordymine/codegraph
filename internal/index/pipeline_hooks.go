package index

// pipelinePreflightErr is set by tests (atomic_test.go) to simulate mid-pipeline failure.
var pipelinePreflightErr error

func pipelinePreflight() error {
	return pipelinePreflightErr
}
