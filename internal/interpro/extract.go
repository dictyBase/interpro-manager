package interpro

import (
	"bytes"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	O "github.com/IBM/fp-go/v2/option"
	S "github.com/IBM/fp-go/v2/string"

	"github.com/dictybase/interpro-manager/internal/seqio"
)

var hasGene = func(r Result) bool {
	return S.IsNonEmpty(r.Metadata.Gene)
}

var toTSVRow = F.Flow2(
	func(r Result) []string {
		return A.From(
			r.Metadata.Accession,
			r.Metadata.Name,
			r.Metadata.Gene,
		)
	},
	S.Join("\t"),
)

func FormatTSVChunk(results []Result) string {
	return F.Pipe6(
		results,
		A.Filter(hasGene),
		A.Map(toTSVRow),
		S.Join("\n"),
		O.FromPredicate(S.IsNonEmpty),
		O.Map(S.Append("\n")),
		O.GetOrElse(F.Constant("")),
	)
}

func extractSeqID(rec seqio.Fasta) string {
	if i := bytes.IndexAny(rec.ID, " \t"); i >= 0 {
		return string(rec.ID[:i])
	}
	return string(rec.ID)
}
