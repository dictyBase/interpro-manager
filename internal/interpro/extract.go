package interpro

import (
	"strings"

	A "github.com/IBM/fp-go/v2/array"
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
	if len(records) == 0 {
		return header
	}

	rows := A.Map(func(r ProteinRecord) string {
		return strings.Join([]string{r.Accession, r.Name, r.Gene}, "\t")
	})(records)

	return header + "\n" + strings.Join(rows, "\n")
}
