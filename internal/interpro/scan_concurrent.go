package interpro

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	T "github.com/IBM/fp-go/v2/tuple"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/semaphore"

	"github.com/dictybase/interpro-manager/internal/seqio"
)

// processOneRecord executes the full submit→poll→download→save pipeline
// for a single FASTA record. Composes the existing pipeline functions.
func processOneRecord(input SubmitInput) E.Either[error, string] {
	return F.Pipe4(
		input,
		buildSubmitRequester,
		IOE.Chain(pollJob),
		IOE.Chain(downloadAndSave),
		toEither[error, string],
	)
}

// ConcurrentScan is the entry point for the concurrent-scan subcommand.
func ConcurrentScan(_ context.Context, cmd *cli.Command) error {
	return F.Pipe8(
		cmd,
		extractConcurrentScanRequest,
		validateScanRequest,
		IOE.FromEither,
		IOE.Map[error](func(scanr ScanRequest) SubmitArgs {
			return T.MakeTuple2(
				ioehttp.MakeClient(
					&http.Client{Timeout: scanr.Timeout}),
				scanr,
			)
		}),
		IOE.ChainFirst(func(args SubmitArgs) IOE.IOEither[error, string] {
			return IOEF.MkdirAll(T.Second(args).OutputDir, outputDirPerm)
		}),
		IOE.Chain(streamFastaRecordsConcurrent),
		toEither[error, []string],
		E.Fold(wrapScanError, reportConcurrentResults),
	)
}

func extractConcurrentScanRequest(cmd *cli.Command) ScanRequest {
	return ScanRequest{
		FastaPath:    cmd.String("fasta"),
		Email:        cmd.String("email"),
		OutputDir:    cmd.String("output"),
		SeqType:      cmd.String("seq-type"),
		BaseURL:      scanBaseURL,
		PollInterval: cmd.Duration("poll-interval"),
		Timeout:      cmd.Duration("timeout"),
		Concurrency:  cmd.Int("concurrency"),
	}
}

func reportConcurrentResults(paths []string) error {
	for _, p := range paths {
		fmt.Printf("wrote %s\n", p)
	}
	return nil
}

// streamFastaRecordsConcurrent reads FASTA records and processes them
// concurrently using a semaphore-bounded worker pool with best-effort
// error handling: individual record failures are collected, not fatal.
func streamFastaRecordsConcurrent(args SubmitArgs) IOE.IOEither[error, []string] {
	return IOE.TryCatchError(func() ([]string, error) {
		client := args.F1
		config := args.F2
		concurrency := config.Concurrency
		if concurrency <= 0 {
			concurrency = 25
		}

		sem := semaphore.NewWeighted(int64(concurrency))

		ctx := context.Background()

		var wg sync.WaitGroup
		var mu sync.Mutex
		var results []string
		var errs []string

		for res := range seqio.ParseFASTA(config.FastaPath) {
			if E.IsLeft(res) {
				mu.Lock()
				errs = append(errs, fmt.Sprintf(
					"fasta parse error: %v",
					leftVal(res),
				))
				mu.Unlock()
				continue
			}
			rec := rightVal(res)

			if err := sem.Acquire(ctx, 1); err != nil {
				return nil, fmt.Errorf(
					"semaphore acquire: %w", err,
				)
			}

			wg.Add(1)
			go func(r seqio.Fasta) {
				defer sem.Release(1)
				defer wg.Done()

				input := T.MakeTuple3(client, config, r)

				result := processOneRecord(input)
				if E.IsLeft(result) {
					mu.Lock()
					errs = append(errs, fmt.Sprintf(
						"seq %s: %v",
						extractSeqID(r),
						leftVal(result),
					))
					mu.Unlock()
				} else {
					mu.Lock()
					results = append(results, rightVal(result))
					mu.Unlock()
				}
			}(rec)
		}

		wg.Wait()

		if len(errs) > 0 {
			return results, fmt.Errorf(
				"%d record(s) failed:\n  %s",
				len(errs), strings.Join(errs, "\n  "),
			)
		}
		return results, nil
	})
}

func leftVal[A, B any](e E.Either[A, B]) A {
	return E.Fold[A, B](F.Identity[A], func(_ B) A {
		var zero A
		return zero
	})(e)
}

func rightVal[A, B any](e E.Either[A, B]) B {
	return E.Fold[A, B](func(_ A) B {
		var zero B
		return zero
	}, F.Identity[B])(e)
}
