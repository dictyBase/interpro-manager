package interpro

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	"github.com/urfave/cli/v3"
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

func TestFormatTSVChunk(t *testing.T) {
	t.Run("with results", func(t *testing.T) {
		results := []Result{
			{Metadata: Metadata{Accession: "A1", Name: "Protein 1", Gene: "geneA"}},
			{Metadata: Metadata{Accession: "A2", Name: "Protein 2", Gene: ""}},
			{Metadata: Metadata{Accession: "A3", Name: "Protein 3", Gene: "geneC"}},
		}

		chunk := FormatTSVChunk(results)
		assert.Equal(t, "A1\tProtein 1\tgeneA\nA3\tProtein 3\tgeneC\n", chunk)
	})

	t.Run("no entries have gene", func(t *testing.T) {
		results := []Result{
			{Metadata: Metadata{Accession: "A1", Name: "Protein 1", Gene: ""}},
			{Metadata: Metadata{Accession: "A2", Name: "Protein 2", Gene: ""}},
		}

		chunk := FormatTSVChunk(results)
		assert.Equal(t, "", chunk)
	})

	t.Run("empty results", func(t *testing.T) {
		chunk := FormatTSVChunk([]Result{})
		assert.Equal(t, "", chunk)
	})
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

func TestFetchPage(t *testing.T) {
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

	result := fetchPage(T.MakeTuple2(ioehttp.MakeClient(server.Client()), server.URL))()

	require.True(t, E.IsRight(result))
	step := unwrapEither(result)
	assert.Equal(t, "", step.F2)
	assert.Contains(t, step.F1, "A1\tProtein 1\tgeneA")
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

func TestWrapDownloadError(t *testing.T) {
	err := wrapDownloadError(fmt.Errorf("network error"))
	assert.EqualError(t, err, "download failed: network error")
}

func TestReportSuccess(t *testing.T) {
	var buf bytes.Buffer

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := reportSuccess("/tmp/output.tsv")

	w.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(&buf, r)

	assert.NoError(t, err)
	assert.Equal(t, "wrote /tmp/output.tsv\n", buf.String())
}

func TestWriteRuntimeHeader(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_header.tsv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	result := writeRuntimeHeader(tmpFile)()
	require.True(t, E.IsRight(result))

	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Equal(t, "accession\tname\tgene\n", string(content))
}

func TestInitialDownloadConfig(t *testing.T) {
	run := false
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "taxon-id"},
			&cli.IntFlag{Name: "page-size", Value: 200},
			&cli.StringFlag{Name: "output"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			run = true
			cfg := initialDownloadConfig(c)
			assert.Contains(t, cfg.F2, "/44689/?page_size=50")
			assert.Contains(t, cfg.F2, baseURL)
			assert.Equal(t, "/tmp/out.tsv", cfg.F3)
			return nil
		},
	}
	err := cmd.Run(context.Background(), []string{
		"app", "--taxon-id", "44689", "--page-size", "50", "--output", "/tmp/out.tsv",
	})
	require.NoError(t, err)
	assert.True(t, run)
}

func TestOnCreateFile(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "proteins.tsv")

	cfg := T.MakeTuple3(
		ioehttp.MakeClient(http.DefaultClient),
		"https://example.com/api/?page_size=200",
		outputPath,
	)

	result := onCreateFile(cfg)()
	require.True(t, E.IsRight(result))

	state := unwrapEither(result)
	assert.Equal(t, outputPath, state.F3)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, "accession\tname\tgene\n", string(content))
}

func TestWritePage(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_page.tsv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	cfg := T.MakeTuple3(tmpFile, "col1\tcol2\tcol3\n", "https://next.page")
	result := writePage(cfg)()

	require.True(t, E.IsRight(result))
	assert.Equal(t, "https://next.page", unwrapEither(result))

	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Contains(t, string(content), "col1\tcol2\tcol3")
}

func TestRunLoop(t *testing.T) {
	callCount := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++

		var next string
		if callCount == 1 {
			next = fmt.Sprintf(`"%s/page2"`, server.URL)
		} else {
			next = "null"
		}

		resp := fmt.Sprintf(
			`{"count":2,"next":%s,"previous":null,"results":[{"metadata":{"accession":"A%d","name":"Protein %d","source_database":"unreviewed","length":100,"source_organism":{"taxId":"1","scientificName":"Org","fullName":"Org"},"gene":"gene%d","in_alphafold":false,"in_bfvd":false},"taxa":[]}]}`,
			next,
			callCount,
			callCount,
			callCount,
		)
		fmt.Fprint(w, resp)
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_loop.tsv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	state := T.MakeTuple4(
		ioehttp.MakeClient(server.Client()),
		server.URL,
		tmpFile.Name(),
		tmpFile,
	)

	result := runLoop(state)()
	require.True(t, E.IsRight(result))

	outPath := unwrapEither(result)
	assert.Equal(t, tmpFile.Name(), outPath)

	content, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "gene1")
	assert.Contains(t, string(content), "gene2")
}

func TestRunLoopFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_loop_err.tsv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	state := T.MakeTuple4(
		ioehttp.MakeClient(server.Client()),
		server.URL,
		tmpFile.Name(),
		tmpFile,
	)

	result := runLoop(state)()
	require.True(t, E.IsLeft(result))
	assert.Contains(t, E.Fold[error, string](F.Identity[error], func(_ string) error {
		panic("expected Left")
	})(result).Error(), "500")
}

func TestRuntimeHandle(t *testing.T) {
	f, err := os.CreateTemp("", "test_handle")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	state := T.MakeTuple4(
		ioehttp.MakeClient(http.DefaultClient),
		"https://example.com",
		"/tmp/out.tsv",
		f,
	)

	assert.Same(t, f, runtimeHandle(state))
}

func TestDownloadAndWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
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
		}`)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "proteins.tsv")

	run := false
	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "taxon-id"},
			&cli.IntFlag{Name: "page-size", Value: 200},
			&cli.StringFlag{Name: "output"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			run = true
			cfg := initialDownloadConfig(c)
			cfg = T.MakeTuple3(ioehttp.MakeClient(server.Client()), server.URL, outputPath)

			err := F.Pipe4(
				cfg,
				onCreateFile,
				func(acquire IOE.IOEither[error, RuntimeState]) IOE.IOEither[error, string] {
					return F.Pipe1(
						runLoop,
						IOE.WithResource[string](
							acquire,
							F.Flow2(runtimeHandle, closeOutputFile),
						),
					)
				},
				toEither[error, string],
				E.Fold(wrapDownloadError, reportSuccess),
			)
			assert.NoError(t, err)
			return nil
		},
	}
	err := cmd.Run(context.Background(), []string{
		"app", "--taxon-id", "44689", "--output", outputPath,
	})
	require.NoError(t, err)
	assert.True(t, run)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "geneA")
}
