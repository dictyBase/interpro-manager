package interpro

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	ioehb "github.com/IBM/fp-go/v2/ioeither/http/builder"
	M "github.com/IBM/fp-go/v2/monoid"
	STR "github.com/IBM/fp-go/v2/string"
)

// pollJob: SubmittedJob → IOEither[error, CompletedJob]
func pollJob(job SubmittedJob) IOE.IOEither[error, CompletedJob] {
	return IOE.TryCatchError(func() (CompletedJob, error) {
		ctx, cancel := context.WithTimeout(context.Background(), job.Config.Timeout)
		defer cancel()

		for {
			var statusErr error
			status := F.Pipe2(
				getJobStatus(job.Client, job.Config.BaseURL, job.JobID),
				toEither[error, string],
				E.Fold(
					func(err error) string { statusErr = err; return "" },
					F.Identity[string],
				),
			)
			if statusErr != nil {
				return CompletedJob{}, statusErr
			}

			fmt.Fprintf(os.Stderr, "job %s (seq %s): %s\n", job.JobID, job.SeqID, status)

			switch status {
			case "FINISHED":
				return CompletedJob(job), nil
			case "RUNNING", "QUEUED", "PENDING":
				select {
				case <-ctx.Done():
					return CompletedJob{}, fmt.Errorf(
						"job %s (seq %s) timed out", job.JobID, job.SeqID,
					)
				case <-time.After(job.Config.PollInterval):
					continue
				}
			default:
				return CompletedJob{}, fmt.Errorf(
					"job %s (seq %s) ended with status: %s",
					job.JobID, job.SeqID, status,
				)
			}
		}
	})
}

// getJobStatus fetches the current status string for a job ID.
func getJobStatus(client ioehttp.Client, baseURL string, jobID string) IOE.IOEither[error, string] {
	return F.Pipe4(
		B.Default,
		F.Pipe1(
			M.ConcatAll(STR.Monoid)(
				[]string{
					baseURL,
					"/status/",
					url.PathEscape(jobID),
				}),
			B.WithURL,
		),
		B.WithHeader("Accept")("text/plain"),
		ioehb.Requester,
		ioehttp.ReadText(client),
	)
}
