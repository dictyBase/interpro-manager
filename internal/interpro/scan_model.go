package interpro

import (
	"time"

	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	T "github.com/IBM/fp-go/v2/tuple"
)

// ScanRequest carries the user-provided inputs extracted from CLI flags.
type ScanRequest struct {
	FastaPath    string
	Email        string
	OutputDir    string
	SeqType      string
	BaseURL      string
	PollInterval time.Duration
	Timeout      time.Duration
}

// SubmittedJob holds state after a successful API submission.
type SubmittedJob struct {
	JobID  string
	SeqID  string
	Client ioehttp.Client
	Config ScanRequest
}

// CompletedJob holds state after polling reports FINISHED.
type CompletedJob struct {
	JobID  string
	SeqID  string
	Client ioehttp.Client
	Config ScanRequest
}

// SubmitArgs bundles the shared HTTP client and scan config.
// Uses project-wide T.Tuple2 convention for arg bundles.
type SubmitArgs = T.Tuple2[ioehttp.Client, ScanRequest]
