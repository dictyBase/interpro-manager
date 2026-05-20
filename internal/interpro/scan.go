package interpro

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	E "github.com/IBM/fp-go/v2/either"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	M "github.com/IBM/fp-go/v2/monoid"
	Pred "github.com/IBM/fp-go/v2/predicate"
	S "github.com/IBM/fp-go/v2/string"
	T "github.com/IBM/fp-go/v2/tuple"
	"github.com/urfave/cli/v3"
)

var stringMonoid = M.MakeMonoid(
	func(a, b string) string {
		var builder strings.Builder
		builder.WriteString(a)
		builder.WriteString(b)
		return builder.String()
	},
	"",
)

const (
	scanBaseURL   = "https://www.ebi.ac.uk/Tools/services/rest/iprscan6"
	outputDirPerm = 0o755
)

func Scan(_ context.Context, cmd *cli.Command) error {
	return F.Pipe8(
		cmd,
		extractScanRequest,
		validateScanRequest,
		IOE.FromEither,
		IOE.Map[error](func(r ScanRequest) SubmitArgs {
			return T.MakeTuple2(
				ioehttp.MakeClient(
					&http.Client{Timeout: r.Timeout}),
				r,
			)
		}),
		IOE.ChainFirst(func(args SubmitArgs) IOE.IOEither[error, string] {
			return IOEF.MkdirAll(T.Second(args).OutputDir, outputDirPerm)
		}),
		IOE.Chain(streamFastaRecords),
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
