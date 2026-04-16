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

func FormatTSV(records []ProteinRecord) string {
	header := "accession\tname\tgene"
	rows := A.Map(func(r ProteinRecord) string {
		return strings.Join([]string{r.Accession, r.Name, r.Gene}, "\t")
	})(records)

	return E.Fold(
		func([]string) string { return header },
		func(rs []string) string { return header + "\n" + strings.Join(rs, "\n") },
	)(E.FromPredicate(
		func(rows []string) bool { return len(rows) > 0 },
		func(rows []string) []string { return rows },
	)(rows))
}
