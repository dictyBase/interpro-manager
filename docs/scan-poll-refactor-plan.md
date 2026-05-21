# `pollJob` Refactoring Plan

## Problems with the current implementation

1. **`IOE.TryCatchError` wraps entire control flow.** It encloses a `for` loop,
   a `switch`, context management, and sleep logic together. `TryCatchError` is
a thin escape hatch for a *single* fallible effect, not a container for a state
machine.

2. **Error-extraction anti-pattern.** The sentinel `statusErr` variable +
   `E.Fold` (lines 28–38 of `scan_poll.go`) manually unpacks an `Either` back
into a plain Go error inside a loop body, breaking compositional purity.

3. **Imperative `switch` for status dispatch.** The Go `switch status { case
   "FINISHED": ... }` block is imperative branching over a string token.
`F.Switch` (`github.com/IBM/fp-go/v2/function`) is designed exactly for this: a
key-extraction function + a handler map + a default fallback.

4. **Mixed concerns in a single body.** HTTP fetch, logging, status dispatch,
   sleep, and timeout are all interleaved with no conceptual separation.

---

## Target architecture

Three type aliases collapse the repeated composed types, then three focused functions replace the existing two.

```
// aliases
pollTick       = O.Option[CompletedJob]
pollTickIO     = IOE.IOEither[error, pollTick]
statusHandler  = func(string) pollTickIO

// functions
buildStatusHandler — pure F.Switch dispatch: statusHandler
tickOnce           — single poll tick as a monadic pipeline (logging inline via IO.Logf)
pollJob            — reduced loop shell: timeout + retry orchestration only
getJobStatus       — unchanged
```

---

## Refactored `scan_poll.go`

```go
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
// The returned function maps a status string to a pollTickIO:
//
//   - "FINISHED"               → Right(Some(CompletedJob))  — loop may terminate
//   - "RUNNING"|"QUEUED"|"PENDING" → Right(None)            — caller should wait and retry
//   - anything else            → Left(error)                — terminal failure
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
		IOE.ChainFirstIOK(IO.Logf[string](logFmt)),
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
```

---

## Key design decisions

### Why `F.Constant1[string, R](value)` per status case?

`F.Switch` requires each map entry to be `func(T) R` — a function that accepts the status string and returns a result. Since `FINISHED`/`RUNNING`/`QUEUED`/`PENDING` all ignore the string value itself, `F.Constant1` is the idiomatic fp-go way to lift a constant value into that shape without writing an explicit closure each time.

### Why `O.Option[CompletedJob]` as the tick result?

This models the three-way outcome of a single tick as a proper type rather than imperative `continue`/`return` jumps:
- `Right(Some(job))` → done
- `Right(None)` → still running, wait and retry
- `Left(err)` → terminal

The loop body folds cleanly over `Either` then `Option` with no extra sentinel variables or Go `switch`.

### Why does the `select` block remain imperative?

Go has no monadic primitive for competing on `context.Done()` and `time.After` concurrently. The `select` is the minimal imperative residue — all status logic above it is fully functional.

### Why is `dispatch` built once outside the loop?

`buildStatusHandler` closes over `job` and constructs the handler map once. Calling it inside the loop would allocate a new map on every tick.

---

## File impact

| File | Change |
|---|---|
| `scan_poll.go` | Full replacement — 5 type aliases + 4 functions instead of 2 functions |
| `scan_loop.go` | **No change** — `pollJob` signature is identical |
| All other files | **No change** |

### New imports added to `scan_poll.go`

```go
IO  "github.com/IBM/fp-go/v2/io"
O   "github.com/IBM/fp-go/v2/option"
```

`F.Switch`, `F.Constant1`, `F.Void`, and `F.VOID` are all available via the existing `F` alias (`github.com/IBM/fp-go/v2/function`). `"os"` is no longer imported.

### Type aliases added to `scan_poll.go`

| Alias | Expanded type | Purpose |
|---|---|---|
| `pollTick` | `O.Option[CompletedJob]` | Per-tick outcome carried by each IOEither |
| `pollTickIO` | `IOE.IOEither[error, pollTick]` | The IOEither type returned by each tick and dispatch case |
| `statusHandler` | `func(string) pollTickIO` | The dispatch function type from `buildStatusHandler` |
| `tickResult` | `T.Tuple3[error, bool, CompletedJob]` | Inspectable fold result: F1=error, F2=done, F3=job |
| `tickInput` | `T.Tuple2[SubmittedJob, statusHandler]` | Single parameter for `tickOnce`, bundles job + dispatch |

---

## Before / After comparison

| Concern | Before | After |
|---|---|---|
| Status dispatch | Go `switch` statement | `F.Switch` + handler map |
| Error unwrapping in loop | Sentinel `statusErr` + `E.Fold` | `E.Fold` + `O.Fold` at loop boundary |
| Logging | Inline `fmt.Fprintf` in loop body | `IOE.ChainFirstIOK(IO.Logf[string](...))` inline in `tickOnce` |
| Single tick concept | No abstraction | `tickOnce` monadic pipeline |
| `TryCatchError` scope | Wraps loop + all branching logic | Wraps loop shell only |
| Imperative `switch` inside loop | Present | Eliminated |
