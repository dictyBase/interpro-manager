package interpro

import (
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	O "github.com/IBM/fp-go/v2/option"
	S "github.com/IBM/fp-go/v2/string"
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
