# InterPro full refactor plan

## Objective

Refactor the current codebase to match the stricter shape you described:

1. Remove `github.com/IBM/fp-go/v2/effect` entirely.
2. Use fp-go HTTP APIs for the full request/decode path.
3. Keep exactly one imperative construct in the runtime path: the pagination `for` loop.
4. End each per-page HTTP pipeline with `E.Fold`, returning a tuple that the loop can inspect.
5. Use tuple arities larger than 2 aggressively to keep helpers mostly unary and point-free.
6. Keep file opening/writing in fp-go file APIs.
7. Keep filtering/extraction/rendering pure.

This revision explicitly aligns with the attached HTTP examples and the local fp-go examples under `/home/agent/fp-go-concepts/v2/http-examples`.

---

## What changes from the previous plan

The previous version still treated the HTTP layer too much like a manual transport layer.
That is not the right target.

The correct target is the exact fp-go HTTP composition pattern shown in the examples:

```go
message := F.Pipe5(
	builder.Default,
	builder.WithURL(url),
	ioehb.Requester,
	ioehttp.ReadJSON[User](client),
	toEither,
	E.Fold(...),
)
```

and also:

```go
return F.Pipe6(
	builder.Default,
	builder.WithURL(url),
	ioehb.Requester,
	ioehttp.ReadJSON[User](client),
	toEither,
	E.Fold(...),
)
```

The key missing piece was this:

- the per-page fetch in the loop should itself be a single fp-go HTTP pipeline,
- that pipeline should end in `E.Fold`,
- that fold should return a tuple carrying the page result or the error state,
- the loop should then act on that tuple.

That is the center of the new plan.

---

## Current problems in the repository

### 1. `effect` adds indirection without benefit

Current usage: `internal/interpro/command.go:9-52`

Problems:

- `ExtractConfig` is wrapped into `EF.Effect` and immediately provided.
- `liftToThunk` exists only to bridge `IOEither` back into `effect`.
- execution is harder to follow than necessary.

### 2. The HTTP layer is not using the fp-go HTTP pipeline shape

Current usage: `internal/interpro/client.go:16-69`

Problems:

- request construction is manual,
- request execution is manual,
- JSON decoding is manual,
- the code does not follow the `builder -> Requester -> ReadJSON -> toEither -> E.Fold` flow.

### 3. Pagination is recursive instead of being the single explicit loop

Current usage: `internal/interpro/client.go:79-98`

Problems:

- recursion hides the runtime iteration boundary,
- all pages are accumulated in memory,
- the code does not stream page data to the output file.

### 4. File writing is whole-buffer oriented

Current usage: `internal/interpro/tsv.go:14-24`

Problems:

- it writes one final payload instead of opening once and appending page chunks,
- it does not fit the single-loop architecture.

### 5. Tuple usage is underpowered

Current code mostly uses structs or `Tuple2`-like state.

That misses a major fp-go ergonomics point:

- tuples can carry execution context,
- tuples can keep helpers unary,
- tuples can keep intermediate transformations point-free,
- tuples can be widened as the pipeline grows.

---

## Target architecture

## Runtime flow

```text
CLI flags
  -> pure tuple config
  -> open file with fp-go
  -> write header with fp-go
  -> for currentURL != "" {
       -> run one fp-go HTTP pipeline
          builder.Default
          -> builder.WithURL(currentURL)
          -> ioehb.Requester
          -> ioehttp.ReadJSON[APIResponse](client)
          -> toEither
          -> E.Fold(...return tuple...)
       -> inspect tuple outside the pipe
       -> write page chunk with fp-go
       -> update currentURL from tuple
     }
  -> close file with fp-go
  -> fold final result to user-facing error/success
```

## Design rule

Everything inside one page iteration should be expressed functionally.
The loop is only the boundary that repeatedly executes the pipeline.

---

## Tuple-first state design

Use tuples much more aggressively than before.

## Proposed tuple aliases

```go
type ExtractConfig = T.Tuple3[ioehttp.Client, string, string]
// F1 client
// F2 startURL
// F3 outputPath

type RuntimeState = T.Tuple4[*os.File, ioehttp.Client, string, string]
// F1 file handle
// F2 client
// F3 current URL
// F4 output path

type PageData = T.Tuple3[APIResponse, []ProteinRecord, string]
// F1 decoded response
// F2 extracted rows
// F3 next URL

type PageStep = T.Tuple4[error, string, []ProteinRecord, string]
// F1 error
// F2 rendered TSV chunk
// F3 extracted rows
// F4 next URL

type LoopStep = T.Tuple3[error, string, string]
// F1 error
// F2 next URL
// F3 output path
```

This is deliberate.
The runtime should prefer tuples over ad hoc temporary structs when the purpose is to thread values through unary transforms.

## Accessors

```go
func configClient(cfg ExtractConfig) ioehttp.Client { return cfg.F1 }
func configStartURL(cfg ExtractConfig) string       { return cfg.F2 }
func configOutputPath(cfg ExtractConfig) string     { return cfg.F3 }

func runtimeHandle(state RuntimeState) *os.File     { return state.F1 }
func runtimeClient(state RuntimeState) ioehttp.Client { return state.F2 }
func runtimeURL(state RuntimeState) string          { return state.F3 }
func runtimeOutputPath(state RuntimeState) string   { return state.F4 }

func pageResponse(data PageData) APIResponse        { return data.F1 }
func pageRows(data PageData) []ProteinRecord        { return data.F2 }
func pageNext(data PageData) string                 { return data.F3 }

func stepError(step PageStep) error                 { return step.F1 }
func stepChunk(step PageStep) string                { return step.F2 }
func stepRows(step PageStep) []ProteinRecord        { return step.F3 }
func stepNext(step PageStep) string                 { return step.F4 }

func loopError(step LoopStep) error                 { return step.F1 }
func loopNext(step LoopStep) string                 { return step.F2 }
func loopOutput(step LoopStep) string               { return step.F3 }
```

The code should not be shy about introducing `Tuple3`, `Tuple4`, and `Tuple5` if they make helpers unary.

---

## HTTP layer: exact target shape

This is the most important correction.

## HTTP rule

The page fetch helper should look like the attached examples:

```go
F.Pipe6(
	builder.Default,
	builder.WithURL(url),
	ioehb.Requester,
	ioehttp.ReadJSON[APIResponse](client),
	toEither,
	E.Fold(...),
)
```

That means:

- request building uses `builder.Default` and `builder.WithURL(url)`,
- request materialization uses `ioehb.Requester`,
- GET execution and JSON decoding use `ioehttp.ReadJSON[APIResponse](client)`,
- lazy evaluation is forced with `toEither`,
- final handling happens in `E.Fold`.

## Generic helper

```go
func toEither[ERR, A any](ioe IOE.IOEither[ERR, A]) E.Either[ERR, A] {
	return ioe()
}
```

## Page request builder

```go
func buildPageRequest(url string) builder.Builder {
	return F.Pipe1(
		builder.Default,
		builder.WithURL(url),
	)
}
```

## Page decode pipeline

```go
func fetchPageStep(client ioehttp.Client) func(string) PageStep {
	return func(url string) PageStep {
		return F.Pipe6(
			builder.Default,
			builder.WithURL(url),
			ioehb.Requester,
			ioehttp.ReadJSON[APIResponse](client),
			toEither,
			E.Fold(
				func(err error) PageStep {
					return T.MakeTuple4(
						err,
						"",
						[]ProteinRecord{},
						"",
					)
				},
				func(resp APIResponse) PageStep {
					return F.Pipe1(
						resp,
						toPageData,
						func(data PageData) PageStep {
							return T.MakeTuple4(
								nil,
								FormatTSVChunk(pageRows(data)),
								pageRows(data),
								pageNext(data),
							)
						},
					)
				},
			),
		)
	}
}
```

This is the right place to end the pipe.
The loop does not decode JSON itself.
The loop only consumes the tuple returned by this fold.

---

## Pure transformation helpers

## Model helpers

```go
func nextURL(next *string) string {
	return O.Fold(
		func() string { return "" },
		func(url string) string { return url },
	)(O.FromNillable(next))
}

func toProteinRecord(r Result) ProteinRecord {
	return ProteinRecord{
		Accession: r.Metadata.Accession,
		Name:      r.Metadata.Name,
		Gene:      r.Metadata.Gene,
	}
}

func hasGene(r Result) bool {
	return r.Metadata.Gene != ""
}

func ExtractRecords(results []Result) []ProteinRecord {
	return F.Pipe2(
		results,
		A.Filter(hasGene),
		A.Map(toProteinRecord),
	)
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

func toPageData(resp APIResponse) PageData {
	return T.MakeTuple3(
		resp,
		ExtractRecords(resp.Results),
		nextURL(resp.Next),
	)
}
```

---

## File layer

All file operations remain fp-go based.

```go
func openOutputFile(path string) IOE.IOEither[error, *os.File] {
	return FILE.Create(path)
}

func closeOutputFile(handle *os.File) IOE.IOEither[error, struct{}] {
	return IOE.TryCatchError(func() (struct{}, error) {
		return struct{}{}, handle.Close()
	})
}

func writeHeader(handle *os.File) IOE.IOEither[error, []byte] {
	return FILE.WriteAll[*os.File]([]byte("accession\tname\tgene\n"))(IOE.Of[error](handle))
}

func writeChunk(handle *os.File) func(string) IOE.IOEither[error, []byte] {
	return func(chunk string) IOE.IOEither[error, []byte] {
		return FILE.WriteAll[*os.File]([]byte(chunk))(IOE.Of[error](handle))
	}
}
```

---

## CLI and config refactor

## `cmd/interpro-cli/main.go`

Move from raw argument passing to tuple config creation.

```go
func buildStartURL(taxonID string, pageSize int) string {
	return fmt.Sprintf("%s%s/?page_size=%d", baseURL, taxonID, pageSize)
}

func buildConfig(cmd *cli.Command) interpro.ExtractConfig {
	return T.MakeTuple3(
		interpro.MakeHTTPClient(),
		buildStartURL(cmd.String("taxon-id"), cmd.Int("page-size")),
		cmd.String("output"),
	)
}

func extractAction(_ context.Context, cmd *cli.Command) error {
	return F.Pipe1(
		buildConfig(cmd),
		interpro.ExtractAndWrite,
	)
}
```

The client belongs in config now.
That keeps later helpers unary.

---

## Runtime orchestration

## `internal/interpro/command.go`

Remove `effect` entirely.

```go
func MakeHTTPClient() ioehttp.Client {
	return ioehttp.MakeClient(http.DefaultClient)
}

func newRuntimeState(cfg ExtractConfig) func(*os.File) RuntimeState {
	return func(handle *os.File) RuntimeState {
		return T.MakeTuple4(
			handle,
			configClient(cfg),
			configStartURL(cfg),
			configOutputPath(cfg),
		)
	}
}

func wrapRunError(err error) error {
	return fmt.Errorf("extract failed: %w", err)
}

func reportSuccess(path string) error {
	fmt.Printf("wrote %s\n", path)
	return nil
}

func ExtractAndWrite(cfg ExtractConfig) error {
	return F.Pipe2(
		runProgram(cfg),
		E.Fold(wrapRunError, reportSuccess),
	)
}

func runProgram(cfg ExtractConfig) E.Either[error, string] {
	return IOE.WithResource[string](
		F.Pipe2(
			openOutputFile(configOutputPath(cfg)),
			IOE.Map(newRuntimeState(cfg)),
			IOE.ChainFirst(func(state RuntimeState) IOE.IOEither[error, []byte] {
				return writeHeader(runtimeHandle(state))
			}),
		),
		func(state RuntimeState) IOE.IOEither[error, struct{}] {
			return closeOutputFile(runtimeHandle(state))
		},
	)(runLoop)()
}
```

Note the key simplification:

- no `EF.Ask`,
- no `EF.Provide`,
- no thunk lifting,
- `IOE.WithResource` owns file lifecycle.

---

## The loop design

This is the core implementation change.

The loop is allowed to be imperative.
Everything inside one page iteration should remain fp-go.

## Preferred loop shape

```go
func pageStepEither(step PageStep) E.Either[error, PageStep] {
	return E.FromPredicate(
		func(s PageStep) bool { return stepError(s) == nil },
		func(s PageStep) error { return stepError(s) },
	)(step)
}

func writeStep(handle *os.File) func(PageStep) E.Either[error, LoopStep] {
	return func(step PageStep) E.Either[error, LoopStep] {
		return F.Pipe2(
			writeChunk(handle)(stepChunk(step))(),
			E.Map(func([]byte) LoopStep {
				return T.MakeTuple3(
					nil,
					stepNext(step),
					"",
				)
			}),
			E.MapLeft(func(err error) error {
				return err
			}),
		)
	}
}

func runLoop(state RuntimeState) IOE.IOEither[error, string] {
	return IOE.TryCatchError(func() (string, error) {
		currentURL := runtimeURL(state)
		handle := runtimeHandle(state)
		client := runtimeClient(state)
		outputPath := runtimeOutputPath(state)
		last := T.MakeTuple3[error, string, string](nil, currentURL, outputPath)

		for currentURL != "" {
			last = F.Pipe3(
				currentURL,
				fetchPageStep(client),
				pageStepEither,
				E.Chain(writeStep(handle)),
				E.Fold(
					func(err error) LoopStep {
						return T.MakeTuple3(err, "", outputPath)
					},
					func(next LoopStep) LoopStep {
						return T.MakeTuple3(loopError(next), loopNext(next), outputPath)
					},
				),
			)

			currentURL = loopNext(last)
		}

		return F.Pipe1(
			last,
			E.FromPredicate(
				func(step LoopStep) bool { return loopError(step) == nil },
				func(step LoopStep) error { return loopError(step) },
			),
			E.Fold(
				func(err error) (string, error) {
					return "", err
				},
				func(step LoopStep) (string, error) {
					return loopOutput(step), nil
				},
			),
		)
	})
}
```

## Why this loop shape is the right one

It matches your requirement exactly:

- the `for` loop is the only imperative construct,
- the per-page work is a single fp-go pipeline,
- the pipeline ends in `E.Fold`,
- `E.Fold` returns a tuple,
- the loop then takes action from that tuple.

This also keeps most helpers unary:

- `fetchPageStep(client) func(string) PageStep`
- `pageStepEither(step) Either[error, PageStep]`
- `writeStep(handle) func(PageStep) Either[error, LoopStep]`

---

## The exact HTTP per-page snippet the refactor should target

This is the most important concrete shape in the whole plan.

```go
func fetchPageStep(client ioehttp.Client) func(string) PageStep {
	return func(url string) PageStep {
		return F.Pipe6(
			builder.Default,
			builder.WithURL(url),
			ioehb.Requester,
			ioehttp.ReadJSON[APIResponse](client),
			toEither,
			E.Fold(
				func(err error) PageStep {
					return T.MakeTuple4(err, "", []ProteinRecord{}, "")
				},
				func(resp APIResponse) PageStep {
					return T.MakeTuple4(
						nil,
						FormatTSVChunk(ExtractRecords(resp.Results)),
						ExtractRecords(resp.Results),
						nextURL(resp.Next),
					)
				},
			),
		)
	}
}
```

Then the loop simply consumes that tuple.

If you want to reduce duplicate `ExtractRecords(resp.Results)` calls, widen the tuple pipeline first:

```go
func toPageData(resp APIResponse) PageData {
	return T.MakeTuple3(
		resp,
		ExtractRecords(resp.Results),
		nextURL(resp.Next),
	)
}

func fetchPageStep(client ioehttp.Client) func(string) PageStep {
	return func(url string) PageStep {
		return F.Pipe6(
			builder.Default,
			builder.WithURL(url),
			ioehb.Requester,
			ioehttp.ReadJSON[APIResponse](client),
			toEither,
			E.Fold(
				func(err error) PageStep {
					return T.MakeTuple4(err, "", []ProteinRecord{}, "")
				},
				func(resp APIResponse) PageStep {
					return F.Pipe1(
						resp,
						toPageData,
						func(data PageData) PageStep {
							return T.MakeTuple4(
								nil,
								FormatTSVChunk(pageRows(data)),
								pageRows(data),
								pageNext(data),
							)
						},
					)
				},
			),
		)
	}
}
```

That is the preferred version.

---

## File-by-file change list

## 1. `cmd/interpro-cli/main.go`

- build `ExtractConfig` as `Tuple3[client,startURL,outputPath]`
- pass tuple directly into `interpro.ExtractAndWrite`

## 2. `internal/interpro/command.go`

- delete all `effect` usage
- create runtime state from config + file handle
- use `IOE.WithResource` for file lifecycle
- call loop runner directly

## 3. `internal/interpro/client.go`

- remove manual HTTP code
- add `toEither`
- add `buildPageRequest`
- add `fetchPageStep(client)` with the exact `Pipe6` + `E.Fold(tuple)` shape

## 4. `internal/interpro/extract.go`

- keep `ExtractRecords`
- keep TSV rendering pure
- add `toPageData`

## 5. `internal/interpro/tsv.go`

- split file handling into `openOutputFile`, `writeHeader`, `writeChunk`, `closeOutputFile`

## 6. `internal/interpro/model.go`

- keep API response models
- add tuple aliases and small accessors

## 7. `internal/interpro/loop.go`

- place the single allowed `for` loop here
- consume tuple returned from `fetchPageStep`
- no recursive pagination

---

## Exact removal list

These must disappear:

```go
EF "github.com/IBM/fp-go/v2/effect"
liftToThunk
ExtractEffect
FetchAllPages
http.NewRequest
http.DefaultClient.Do
json.Unmarshal
assertOK
readBody
fetchURL
```

These must replace them:

```go
builder.Default
builder.WithURL
ioehb.Requester
ioehttp.ReadJSON[APIResponse]
toEither
E.Fold(...return tuple...)
Tuple3
Tuple4
IOE.WithResource
FILE.Create
FILE.WriteAll
```

---

## Test plan

## A. Builder request test

```go
func TestBuildPageRequest(t *testing.T) {
	result := F.Pipe3(
		builder.Default,
		builder.WithURL("https://example.org/page"),
		ioehb.Requester,
		toEither,
	)

	require.True(t, E.IsRight(result))
	req := unwrapEither(result)
	assert.Equal(t, "https://example.org/page", req.URL.String())
}
```

## B. fp-go HTTP JSON decode test

```go
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

	step := fetchPageStep(ioehttp.MakeClient(server.Client()))(server.URL)

	assert.NoError(t, stepError(step))
	assert.Equal(t, "", stepNext(step))
	assert.Contains(t, stepChunk(step), "A1\tProtein 1\tgeneA")
}
```

## C. File write test

```go
func TestWriteChunk(t *testing.T) {
	tmpDir := t.TempDir()
	path := File.Join("proteins.tsv")(tmpDir)

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
				return FILE.ReadFile(path)
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
```

## D. Full loop test

```go
func TestExtractAndWriteLoopsAcrossPages(t *testing.T) {
	page2 := `{
		"count": 3,
		"next": null,
		"previous": "ignored",
		"results": [
			{
				"metadata": {
					"accession": "A3",
					"name": "Protein 3",
					"source_database": "unreviewed",
					"length": 100,
					"source_organism": {
						"taxId": "44689",
						"scientificName": "Dictyostelium discoideum",
						"fullName": "Dictyostelium discoideum"
					},
					"gene": "geneC",
					"in_alphafold": false,
					"in_bfvd": false
				},
				"taxa": []
			}
		]
	}`

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.RawQuery == "page=2" {
			_, _ = w.Write([]byte(page2))
			return
		}

		response := fmt.Sprintf(`{
			"count": 3,
			"next": %q,
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
				},
				{
					"metadata": {
						"accession": "A2",
						"name": "Protein 2",
						"source_database": "unreviewed",
						"length": 100,
						"source_organism": {
							"taxId": "44689",
							"scientificName": "Dictyostelium discoideum",
							"fullName": "Dictyostelium discoideum"
						},
						"gene": "",
						"in_alphafold": false,
						"in_bfvd": false
					},
					"taxa": []
				}
			]
		}`, server.URL+"/?page=2")

		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	path := File.Join("proteins.tsv")(tmpDir)

	err := ExtractAndWrite(T.MakeTuple3(
		ioehttp.MakeClient(server.Client()),
		server.URL,
		path,
	))
	require.NoError(t, err)

	content := FILE.ReadFile(path)()
	require.True(t, E.IsRight(content))
	assert.Equal(
		t,
		"accession\tname\tgene\nA1\tProtein 1\tgeneA\nA3\tProtein 3\tgeneC\n",
		string(unwrapEither(content)),
	)
}
```

---

## Recommended implementation order

1. Replace `ExtractConfig` with `Tuple3[client,startURL,outputPath]`.
2. Delete all `effect` usage.
3. Add tuple aliases and accessors in `model.go` or a new `types.go`.
4. Rewrite `client.go` around the exact `Pipe6 -> E.Fold(tuple)` HTTP pattern.
5. Split file handling into fp-go resource helpers.
6. Introduce `loop.go` with the single allowed `for` loop.
7. Update tests to prove the HTTP pipeline and the tuple-driven loop behavior.
8. Run tests and lint.

---

## Final checklist

- [ ] `effect` removed.
- [ ] one explicit pagination `for` loop.
- [ ] per-page HTTP fetch is `builder.Default -> builder.WithURL -> ioehb.Requester -> ioehttp.ReadJSON -> toEither -> E.Fold`.
- [ ] `E.Fold` returns a tuple consumed by the loop.
- [ ] `Tuple3`/`Tuple4` are used for config, runtime state, page state, and loop state.
- [ ] file open/write/close use fp-go file APIs.
- [ ] extraction/filtering/rendering are pure.
- [ ] no recursive pagination remains.
- [ ] tests cover builder pipeline, fold-to-tuple page step, and full multi-page write flow.

---

## Commands

```bash
gotestsum --format pkgname-and-test-fails --format-hide-empty-pkg -- ./...
golangci-lint run ./...
```
