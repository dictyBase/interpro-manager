package interpro

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"os"

	E "github.com/IBM/fp-go/v2/either"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	O "github.com/IBM/fp-go/v2/option"
	Pred "github.com/IBM/fp-go/v2/predicate"
	S "github.com/IBM/fp-go/v2/string"
	T "github.com/IBM/fp-go/v2/tuple"
	"github.com/urfave/cli/v3"
)

const (
	scanBaseURL   = "https://www.ebi.ac.uk/Tools/services/rest/iprscan6"
	outputDirPerm = 0o755
)

var (
	hasEmail = F.Pipe1(
		S.IsNonEmpty,
		Pred.ContraMap(func(s ScanRequest) string {
			return s.Email
		}),
	)
	hasFastaPath = F.Pipe1(
		S.IsNonEmpty,
		Pred.ContraMap(func(s ScanRequest) string {
			return s.FastaPath
		}),
	)
)

func Scan(_ context.Context, cmd *cli.Command) error {
	return F.Pipe8(
		cmd,
		extractScanRequest,
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

func validateScanRequest(scanReq ScanRequest) E.Either[error, ScanRequest] {
	return F.Pipe4(
		E.Of[error](scanReq),
		E.Chain(
			E.FromPredicate(
				hasEmail,
				ER.OnSome[ScanRequest]("email is required"),
			)),
		E.Chain(validateEmailFormat),
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

func validateEmailFormat(s ScanRequest) E.Either[error, ScanRequest] {
	addr, err := mail.ParseAddress(s.Email)
	return F.Pipe3(
		O.FromNillable(addr),
		O.Map(func(a *mail.Address) string { return a.Address }),
		E.FromOption[string](func() error {
			return fmt.Errorf("invalid email: %s: %w", s.Email, err)
		}),
		E.Map[error](func(normalized string) ScanRequest {
			s.Email = normalized
			return s
		}),
	)
}

func ensureOutputDir(dir string) IOE.IOEither[error, string] {
	return IOEF.MkdirAll(dir, outputDirPerm)
}
