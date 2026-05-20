package interpro

import (
	"strings"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	T "github.com/IBM/fp-go/v2/tuple"

	"github.com/dictybase-docker/interpro-manager/internal/seqio"
)

func streamFastaRecords(args SubmitArgs) IOE.IOEither[error, []string] {
	return IOE.TryCatchError(func() ([]string, error) {
		var results []string
		var loopErr error

		for res := range seqio.ParseFASTA(T.Second(args).FastaPath) {
			outPath := F.Pipe4(
				res,
				IOE.FromEither,
				IOE.Chain(func(rec seqio.Fasta) IOE.IOEither[error, string] {
					return F.Pipe4(
						buildSubmitRequester(args.F2, rec),
						ioehttp.ReadText(args.F1),
						IOE.Map[error](func(jobID string) SubmittedJob {
							return SubmittedJob{
								JobID:  strings.TrimSpace(jobID),
								SeqID:  extractSeqID(rec),
								Client: args.F1,
								Config: args.F2,
							}
						}),
						IOE.Chain(pollJob),
						IOE.Chain(downloadAndSave),
					)
				}),
				toEither[error, string],
				E.Fold(
					func(err error) string { loopErr = err; return "" },
					func(p string) string { results = append(results, p); return p },
				),
			)
			_ = outPath
			if loopErr != nil {
				return nil, loopErr
			}
		}
		return results, nil
	})
}

