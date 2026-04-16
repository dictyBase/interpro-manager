package interpro

import (
	"encoding/json"
	"os"
	"testing"

	E "github.com/IBM/fp-go/v2/either"
	O "github.com/IBM/fp-go/v2/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unwrapEither[ERR any, A any](e E.Either[ERR, A]) A {
	return E.Fold(
		func(_ ERR) A { panic("unwrapEither called on Left") },
		func(a A) A { return a },
	)(e)
}

func TestDecodeResponse(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		data, err := os.ReadFile("../../docs/interpro_api_response.json")
		require.NoError(t, err)

		result := decodeResponse(data)
		assert.True(t, E.IsRight(result))

		resp := unwrapEither(result)
		assert.Equal(t, 13188, resp.Count)
		assert.NotNil(t, resp.Next)
		assert.Equal(t, "https://www.ebi.ac.uk/interpro/api/protein/UniProt/taxonomy/uniprot/44689/?cursor=source%7Cs%7Cb0g0y9&page_size=20", *resp.Next)
		assert.Nil(t, resp.Previous)
		assert.Equal(t, 20, len(resp.Results))
	})

	t.Run("invalid json", func(t *testing.T) {
		result := decodeResponse([]byte("not json"))
		assert.True(t, E.IsLeft(result))
	})

	t.Run("response with empty gene", func(t *testing.T) {
		jsonData := `{"count":1,"next":null,"previous":null,"results":[{"metadata":{"accession":"A0A0K2SR10","name":"Test Protein","source_database":"unreviewed","length":713,"source_organism":{"taxId":"44689","scientificName":"Dictyostelium discoideum","fullName":"Dictyostelium discoideum (Social amoeba)"},"gene":"","in_alphafold":false,"in_bfvd":false},"taxa":[]}]}`
		result := decodeResponse([]byte(jsonData))
		assert.True(t, E.IsRight(result))
		resp := unwrapEither(result)
		assert.Equal(t, 1, len(resp.Results))
		assert.Equal(t, "", resp.Results[0].Metadata.Gene)
	})
}

func TestExtractRecords(t *testing.T) {
	t.Run("filters out entries without gene", func(t *testing.T) {
		results := []Result{
			{Metadata: Metadata{Accession: "A1", Name: "Protein 1", Gene: "geneA"}},
			{Metadata: Metadata{Accession: "A2", Name: "Protein 2", Gene: ""}},
			{Metadata: Metadata{Accession: "A3", Name: "Protein 3", Gene: "geneC"}},
		}

		records := ExtractRecords(results)
		assert.Equal(t, 2, len(records))
		assert.Equal(t, "A1", records[0].Accession)
		assert.Equal(t, "geneA", records[0].Gene)
		assert.Equal(t, "A3", records[1].Accession)
		assert.Equal(t, "geneC", records[1].Gene)
	})

	t.Run("all entries have gene", func(t *testing.T) {
		results := []Result{
			{Metadata: Metadata{Accession: "A1", Name: "Protein 1", Gene: "geneA"}},
		}

		records := ExtractRecords(results)
		assert.Equal(t, 1, len(records))
	})

	t.Run("no entries have gene", func(t *testing.T) {
		results := []Result{
			{Metadata: Metadata{Accession: "A1", Name: "Protein 1", Gene: ""}},
			{Metadata: Metadata{Accession: "A2", Name: "Protein 2", Gene: ""}},
		}

		records := ExtractRecords(results)
		assert.Equal(t, 0, len(records))
	})

	t.Run("empty results", func(t *testing.T) {
		records := ExtractRecords([]Result{})
		assert.Equal(t, 0, len(records))
	})
}

func TestFormatTSV(t *testing.T) {
	t.Run("with records", func(t *testing.T) {
		records := []ProteinRecord{
			{Accession: "A0A0K2SR10", Name: "Protein A", Gene: "hapA"},
			{Accession: "B0G0Y4", Name: "Protein B", Gene: "wrn"},
		}

		tsv := FormatTSV(records)
		expected := "accession\tname\tgene\nA0A0K2SR10\tProtein A\thapA\nB0G0Y4\tProtein B\twrn"
		assert.Equal(t, expected, tsv)
	})

	t.Run("empty records", func(t *testing.T) {
		tsv := FormatTSV([]ProteinRecord{})
		assert.Equal(t, "accession\tname\tgene", tsv)
	})
}

func TestNextOption(t *testing.T) {
	t.Run("nil pointer", func(t *testing.T) {
		opt := nextOption(nil)
		assert.True(t, O.IsNone(opt))
	})

	t.Run("non-nil pointer", func(t *testing.T) {
		val := "https://example.com/next"
		opt := nextOption(&val)
		assert.True(t, O.IsSome(opt))
	})

	t.Run("extract value", func(t *testing.T) {
		val := "https://example.com/next"
		opt := nextOption(&val)
		result := O.Fold(
			func() string { return "" },
			func(s string) string { return s },
		)(opt)
		assert.Equal(t, "https://example.com/next", result)
	})
}

func TestHasGene(t *testing.T) {
	assert.True(t, hasGene(Result{Metadata: Metadata{Gene: "hapA"}}))
	assert.False(t, hasGene(Result{Metadata: Metadata{Gene: ""}}))
}

func TestToProteinRecord(t *testing.T) {
	r := Result{Metadata: Metadata{Accession: "A1", Name: "Protein", Gene: "geneA"}}
	pr := toProteinRecord(r)
	assert.Equal(t, "A1", pr.Accession)
	assert.Equal(t, "Protein", pr.Name)
	assert.Equal(t, "geneA", pr.Gene)
}

func TestFullPipelineFromSampleJSON(t *testing.T) {
	data, err := os.ReadFile("../../docs/interpro_api_response.json")
	require.NoError(t, err)

	decoded := decodeResponse(data)
	require.True(t, E.IsRight(decoded))

	resp := unwrapEither(decoded)
	records := ExtractRecords(resp.Results)

	assert.Equal(t, 20, len(records))

	assert.Equal(t, "A0A0K2SR10", records[0].Accession)
	assert.Equal(t, "Generative cell specific-1/HAP2 domain-containing protein", records[0].Name)
	assert.Equal(t, "hapA", records[0].Gene)

	tsv := FormatTSV(records)
	assert.Contains(t, tsv, "accession\tname\tgene")
	assert.Contains(t, tsv, "A0A0K2SR10")
	assert.Contains(t, tsv, "hapA")
}

func TestWriteTSV(t *testing.T) {
	t.Run("writes TSV file", func(t *testing.T) {
		tmpFile, err := os.CreateTemp("", "interpro_test_*.tsv")
		require.NoError(t, err)
		defer func() { _ = os.Remove(tmpFile.Name()) }()
		_ = tmpFile.Close()

		records := []ProteinRecord{
			{Accession: "A1", Name: "Protein 1", Gene: "geneA"},
			{Accession: "B2", Name: "Protein 2", Gene: "geneB"},
		}

		result := WriteTSV(tmpFile.Name(), records)()
		assert.True(t, E.IsRight(result))

		content, err := os.ReadFile(tmpFile.Name())
		require.NoError(t, err)
		assert.Contains(t, string(content), "accession\tname\tgene")
		assert.Contains(t, string(content), "A1\tProtein 1\tgeneA")
		assert.Contains(t, string(content), "B2\tProtein 2\tgeneB")
	})
}

func TestDecodeResponseWithMissingGene(t *testing.T) {
	jsonData := `{"count":1,"next":null,"previous":null,"results":[{"metadata":{"accession":"X1","name":"No Gene Protein","source_database":"unreviewed","length":100,"source_organism":{"taxId":"44689","scientificName":"Dictyostelium discoideum","fullName":"Dictyostelium discoideum (Social amoeba)"},"in_alphafold":false,"in_bfvd":false},"taxa":[]}]}`
	var resp APIResponse
	err := json.Unmarshal([]byte(jsonData), &resp)
	require.NoError(t, err)
	assert.Equal(t, "", resp.Results[0].Metadata.Gene)

	records := ExtractRecords(resp.Results)
	assert.Equal(t, 0, len(records))
}
