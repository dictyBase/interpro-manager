package interpro

import (
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
)

func ExtractRecords(results []Result) []ProteinRecord {
	return F.Pipe2(
		results,
		A.Filter(hasGene),
		A.Map(toProteinRecord),
	)
}

func hasGene(r Result) bool {
	return r.Metadata.Gene != ""
}

func toProteinRecord(r Result) ProteinRecord {
	return ProteinRecord{
		Accession: r.Metadata.Accession,
		Name:      r.Metadata.Name,
		Gene:      r.Metadata.Gene,
	}
}

func FormatTSVChunk(records []ProteinRecord) string {
	return E.Fold(
		func([]ProteinRecord) string {
			return ""
		},
		func(rows []ProteinRecord) string {
			return strings.Join(
				A.Map(func(r ProteinRecord) string {
					return strings.Join([]string{r.Accession, r.Name, r.Gene}, "\t")
				})(rows),
				"\n",
			) + "\n"
		},
	)(E.FromPredicate(
		func(rs []ProteinRecord) bool { return len(rs) > 0 },
		func(rs []ProteinRecord) []ProteinRecord { return rs },
	)(records))
}
