package interpro

import (
	"context"
	"fmt"
	"net/http"
	"os"

	E "github.com/IBM/fp-go/v2/either"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	Pred "github.com/IBM/fp-go/v2/predicate"
	S "github.com/IBM/fp-go/v2/string"
	T "github.com/IBM/fp-go/v2/tuple"
	"github.com/urfave/cli/v3"
)

const (
	scanBaseURL   = "https://www.ebi.ac.uk/Tools/services/rest/iprscan6"
	outputDirPerm = 0o755
)

func Scan(_ context.Context, cmd *cli.Command) error {
	return F.Pipe4(
		cmd,
		extractScanRequest,
		scanSequences,
		toEither[error, []string],
		E.Fold(wrapScanError, reportScanResults),
	)
}

func extractScanRequest(cmd *cli.Command) ScanRequest {
	return ScanRequest{
		FastaPath:    cmd.String("fasta"),
		Email:        cmd.String("email"),
		OutputDir:    cmd.String("output"),
		SeqType:      cmd.String("seq-type"),
		BaseURL:      scanBaseURL,
		PollInterval: cmd.Duration("poll-interval"),
		Timeout:      cmd.Duration("timeout"),
	}
}

func wrapScanError(err error) error {
	return fmt.Errorf("scan failed: %w", err)
}

func reportScanResults(paths []string) error {
	for _, p := range paths {
		fmt.Printf("wrote %s\n", p)
	}
	return nil
}

func scanSequences(r ScanRequest) IOE.IOEither[error, []string] {
	client := ioehttp.MakeClient(&http.Client{Timeout: r.Timeout})
	args := T.MakeTuple2(client, r)

	return F.Pipe1(
		IOE.FromEither(validateScanRequest(r)),
		IOE.Chain(func(_ ScanRequest) IOE.IOEither[error, []string] {
			return F.Pipe1(
				ensureOutputDir(r.OutputDir),
				IOE.Chain(func(_ string) IOE.IOEither[error, []string] {
					return streamFastaRecords(r.FastaPath, args)
				}),
			)
		}),
	)
}

var (
	hasEmail = F.Pipe1(
		S.IsNonEmpty,
		Pred.ContraMap(func(s ScanRequest) string { return s.Email }),
	)
	hasFastaPath = F.Pipe1(
		S.IsNonEmpty,
		Pred.ContraMap(func(s ScanRequest) string { return s.FastaPath }),
	)
)

func validateScanRequest(scanReq ScanRequest) E.Either[error, ScanRequest] {
	return F.Pipe3(
		E.Of[error](scanReq),
		E.Chain(
			E.FromPredicate(
				hasEmail,
				ER.OnSome[ScanRequest]("email is required"),
			)),
		E.Chain(E.FromPredicate(
			hasFastaPath,
			ER.OnSome[ScanRequest]("fasta path is required"),
		)),
		E.Chain(func(s ScanRequest) E.Either[error, ScanRequest] {
			return F.Pipe4(
				s.FastaPath,
				IOEF.Stat,
				toEither[error, os.FileInfo],
				E.MapTo[error, os.FileInfo](s),
				E.MapLeft[ScanRequest](func(err error) error {
					return fmt.Errorf(
						"fasta file not found: %s: %w",
						s.FastaPath,
						err,
					)
				}),
			)
		}),
	)
}

func ensureOutputDir(dir string) IOE.IOEither[error, string] {
	return IOEF.MkdirAll(dir, outputDirPerm)
}
