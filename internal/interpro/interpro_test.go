package interpro

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	ioehb "github.com/IBM/fp-go/v2/ioeither/http/builder"
	T "github.com/IBM/fp-go/v2/tuple"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unwrapEither[ERR any, A any](e E.Either[ERR, A]) A {
	return E.Fold(
		func(_ ERR) A { panic("unwrapEither called on Left") },
		func(a A) A { return a },
	)(e)
}

func TestNextURL(t *testing.T) {
	t.Run("nil pointer", func(t *testing.T) {
		assert.Equal(t, "", nextURL(nil))
	})

	t.Run("non-nil pointer", func(t *testing.T) {
		val := "https://example.com/next"
		assert.Equal(t, "https://example.com/next", nextURL(&val))
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

func TestFormatTSVChunk(t *testing.T) {
	t.Run("with records", func(t *testing.T) {
		records := []ProteinRecord{
			{Accession: "A0A0K2SR10", Name: "Protein A", Gene: "hapA"},
			{Accession: "B0G0Y4", Name: "Protein B", Gene: "wrn"},
		}

		chunk := FormatTSVChunk(records)
		assert.Equal(t, "A0A0K2SR10\tProtein A\thapA\nB0G0Y4\tProtein B\twrn\n", chunk)
	})

	t.Run("empty records", func(t *testing.T) {
		chunk := FormatTSVChunk([]ProteinRecord{})
		assert.Equal(t, "", chunk)
	})
}

func TestExtractRecordsFromResponse(t *testing.T) {
	next := "https://example.com/next"
	resp := APIResponse{
		Count: 2,
		Next:  &next,
		Results: []Result{
			{Metadata: Metadata{Accession: "A1", Name: "Protein 1", Gene: "geneA"}},
			{Metadata: Metadata{Accession: "A2", Name: "Protein 2", Gene: ""}},
		},
	}

	records := ExtractRecords(resp.Results)
	assert.Equal(t, 1, len(records))
	assert.Equal(t, "A1", records[0].Accession)
	assert.Equal(t, "https://example.com/next", nextURL(resp.Next))
}

func TestBuildPageRequest(t *testing.T) {
	result := F.Pipe3(
		B.Default,
		B.WithURL("https://example.org/page"),
		ioehb.Requester,
		toEither,
	)

	require.True(t, E.IsRight(result))
	req := unwrapEither(result)
	assert.Equal(t, "https://example.org/page", req.URL.String())
}

func TestFetchPageStep(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"count": 1,
			"next": null,
			"previous": null,
			"results": [
				{
					"metadata": {
						"accession": "A1",
						"name": "Protein 1",
						"source_database": "unreviewed",
						"length": 100,
						"source_organism": {
							"taxId": "44689",
							"scientificName": "Dictyostelium discoideum",
							"fullName": "Dictyostelium discoideum"
						},
						"gene": "geneA",
						"in_alphafold": false,
						"in_bfvd": false
					},
					"taxa": []
				}
			]
		}`))
	}))
	defer server.Close()

	step := fetchPageStep(T.MakeTuple2(ioehttp.MakeClient(server.Client()), server.URL))

	assert.NoError(t, stepError(step))
	assert.Equal(t, "", stepNext(step))
	assert.Contains(t, stepChunk(step), "A1\tProtein 1\tgeneA")
}

func TestWriteChunk(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "proteins.tsv")

	result := IOE.WithResource[[]byte](
		openOutputFile(path),
		closeOutputFile,
	)(func(handle *os.File) IOE.IOEither[error, []byte] {
		return F.Pipe2(
			writeHeader(handle),
			IOE.Chain(func([]byte) IOE.IOEither[error, []byte] {
				return writeChunk(handle)("A1\tProtein 1\tgeneA\n")
			}),
			IOE.Chain(func([]byte) IOE.IOEither[error, []byte] {
				return IOE.FromEither[error](readFile(path))
			}),
		)
	})()

	require.True(t, E.IsRight(result))
	assert.Equal(
		t,
		"accession\tname\tgene\nA1\tProtein 1\tgeneA\n",
		string(unwrapEither(result)),
	)
}
