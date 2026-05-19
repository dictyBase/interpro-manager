package interpro

import (
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"

	"github.com/dictybase-docker/interpro-manager/internal/seqio"
)

// processOneFasta: Fasta → IOEither[error, string]
//
// Composed as a single F.Flow3 of three named Kleisli arrows.
func processOneFasta(args SubmitArgs) func(seqio.Fasta) IOE.IOEither[error, string] {
	return F.Flow3(
		submitOneRecord(args),
		IOE.Chain(pollJob),
		IOE.Chain(downloadAndSave),
	)
}
