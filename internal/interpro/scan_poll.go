package interpro

import (
	"context"
	"fmt"
	"net/url"
	"time"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
	IO "github.com/IBM/fp-go/v2/io"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	ioehb "github.com/IBM/fp-go/v2/ioeither/http/builder"
	M "github.com/IBM/fp-go/v2/monoid"
	O "github.com/IBM/fp-go/v2/option"
	STR "github.com/IBM/fp-go/v2/string"
	T "github.com/IBM/fp-go/v2/tuple"
)

type (
	// pollTick is the per-tick outcome: Some(job) = done, None = retry.
	pollTick = O.Option[CompletedJob]
	// pollTickIO is one tick's IOEither carrying a pollTick.
	pollTickIO = IOE.IOEither[error, pollTick]
	// statusHandler is the dispatch function produced by buildStatusHandler.
	statusHandler = func(string) pollTickIO
	// tickResult captures the three-way outcome of a single poll tick:
	// F1=error (terminal failure), F2=done flag, F3=CompletedJob (valid when F2=true).
	tickResult = T.Tuple3[error, bool, CompletedJob]
	// tickInput bundles the job and its dispatch handler as a single pipe-compatible parameter.
	tickInput = T.Tuple2[SubmittedJob, statusHandler]
)

// buildStatusHandler returns a status-dispatch function built with F.Switch.
func buildStatusHandler(job SubmittedJob) statusHandler {
	return F.Switch[string, string, pollTickIO](
		F.Identity[string],
		map[string]statusHandler{
			"FINISHED": F.Constant1[string, pollTickIO](IOE.Of[error](O.Some(CompletedJob(job)))),
			"RUNNING":  F.Constant1[string, pollTickIO](IOE.Of[error](O.None[CompletedJob]())),
			"QUEUED":   F.Constant1[string, pollTickIO](IOE.Of[error](O.None[CompletedJob]())),
			"PENDING":  F.Constant1[string, pollTickIO](IOE.Of[error](O.None[CompletedJob]())),
		},
		func(status string) pollTickIO {
			return IOE.Left[pollTick](fmt.Errorf(
				"job %s (seq %s) ended with status: %s",
				job.JobID, job.SeqID, status,
			))
		},
	)
}

// tickOnce performs a single poll tick: fetch status, log it via IO.Logf, dispatch via switch.
func tickOnce(input tickInput) pollTickIO {
	job, dispatch := input.F1, input.F2
	logFmt := M.ConcatAll(STR.Monoid)([]string{"job ", job.JobID, " (seq ", job.SeqID, "): %s"})
	return F.Pipe3(
		job,
		getJobStatus,
		IOE.ChainFirstIOK[error, string](IO.Logf[string](logFmt)),
		IOE.Chain(dispatch),
	)
}

func tickFailed(err error) tickResult {
	return T.MakeTuple3[error, bool, CompletedJob](err, false, CompletedJob{})
}

func tickRetry() tickResult {
	return T.MakeTuple3[error, bool, CompletedJob](nil, false, CompletedJob{})
}

func tickDone(c CompletedJob) tickResult {
	return T.MakeTuple3[error, bool, CompletedJob](nil, true, c)
}

// pollJob: SubmittedJob → IOEither[error, CompletedJob]
// Reduced to a pure loop shell — all status-branching logic lives in buildStatusHandler.
func pollJob(job SubmittedJob) IOE.IOEither[error, CompletedJob] {
	return IOE.TryCatchError(func() (CompletedJob, error) {
		ctx, cancel := context.WithTimeout(context.Background(), job.Config.Timeout)
		defer cancel()

		dispatch := buildStatusHandler(job)

		for {
			tick := F.Pipe3(
				T.MakeTuple2(job, dispatch),
				tickOnce,
				toEither[error, pollTick],
				E.Fold[error, pollTick, tickResult](
					tickFailed,
					O.Fold[CompletedJob, tickResult](tickRetry, tickDone),
				),
			)

			if tick.F1 != nil {
				return CompletedJob{}, tick.F1
			}
			if tick.F2 {
				return tick.F3, nil
			}

			select {
			case <-ctx.Done():
				return CompletedJob{}, fmt.Errorf(
					"job %s (seq %s) timed out", job.JobID, job.SeqID,
				)
			case <-time.After(job.Config.PollInterval):
			}
		}
	})
}

func statusURL(job SubmittedJob) string {
	return M.ConcatAll(STR.Monoid)([]string{
		job.Config.BaseURL,
		"/status/",
		url.PathEscape(job.JobID),
	})
}

// getJobStatus fetches the current status string for a job ID.
func getJobStatus(job SubmittedJob) IOE.IOEither[error, string] {
	return F.Pipe4(
		B.Default,
		F.Pipe2(job, statusURL, B.WithURL),
		B.WithHeader("Accept")("text/plain"),
		ioehb.Requester,
		ioehttp.ReadText(job.Client),
	)
}
