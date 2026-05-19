package interpro

import (
	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	T "github.com/IBM/fp-go/v2/tuple"

	"github.com/dictybase-docker/interpro-manager/internal/seqio"
)

func streamFastaRecords(args SubmitArgs) IOE.IOEither[error, []string] {
	return IOE.TryCatchError(func() ([]string, error) {
		var results []string
		var loopErr error

		for res := range seqio.ParseFASTA(T.Second(args).FastaPath) {
			outPath := F.Pipe6(
				res,
				IOE.FromEither,
				IOE.Chain(submitOneRecord(args)),
				IOE.Chain(pollJob),
				IOE.Chain(downloadAndSave),
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
