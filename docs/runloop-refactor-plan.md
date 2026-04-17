# Refactoring Plan: `runLoop` and its helpers

## Root cause

`fetchPageStep` **escapes IOEither early** by calling `toEither()` synchronously, then folds
the result into a flat `PageStep` tuple with an error baked into field `F1`. Everything
downstream has to undo this: `pageStepEither` re-lifts the flat error back to `Either`,
`writeStep` escapes IOEither a second time with `writeChunk(...)()`, and `E.Fold` collapses
the mess into a `LoopStep` carrying three fields — one of which (`F3 outputPath`) never
changes and two of which exist only because errors can't travel naturally through the type.
Staying in IOEither eliminates all of this.

---

## Step 1 — Simplify `PageStep` in `model.go`

**Current:** `T.Tuple4[error, string, []ProteinRecord, string]`

| Field | Value | Problem |
|-------|-------|---------|
| `F1` | error | Belongs in IOEither, not in the data type |
| `F2` | TSV chunk | Keep |
| `F3` | `[]ProteinRecord` | Dead field — computed inside `fetchPageStep`, never accessed by any accessor (`stepRecords` doesn't exist), already consumed by `FormatTSVChunk` |
| `F4` | nextURL | Keep |

**New:** `T.Tuple2[string, string]`

| Field | Value |
|-------|-------|
| `F1` | TSV chunk |
| `F2` | nextURL |

**Delete from `model.go`:** accessor functions `stepError`, `stepChunk`, `stepNext` — they are
one-liner wrappers around `.F1` / `.F2` / `.F4`. After this change callers use direct field
access (`.F1`, `.F2`).

---

## Step 2 — Delete `LoopStep` entirely from `model.go`

**Current:** `T.Tuple3[error, string, string]`

| Field | Value | Problem |
|-------|-------|---------|
| `F1` | error | Belongs in IOEither |
| `F2` | nextURL | Only value that actually needs threading |
| `F3` | outputPath | Captured from `state.F3`, passed through unchanged every iteration; never mutated |

With errors in IOEither and `outputPath` held in the outer closure, the loop threads only
`currentURL` — a plain `string`. The type is pointless.

**Delete from `model.go`:** `LoopStep`, `loopError`, `loopNext`, `loopOutput`.

Keep `runtimeHandle` — still used in `command.go`.

---

## Step 3 — Simplify `FormatTSVChunk` and delete helpers in `extract.go`

`FormatTSVChunk` currently uses `E.FromPredicate` + `E.Fold` purely to branch on an empty
slice — combinators from the error-handling layer pressed into service for a trivial nil-guard.
The function should be a plain filter → map → join.

At the same time, `ExtractRecords` exists only to bridge `[]Result` → `[]ProteinRecord`
before passing to `FormatTSVChunk`. If `FormatTSVChunk` accepts `[]Result` directly, the
intermediate struct and its helpers become dead code.

**Delete from `extract.go`:** `ExtractRecords`, `toProteinRecord`.

**Delete from `model.go`:** `ProteinRecord` — no longer referenced anywhere.

**Keep:** `hasGene` is deleted too — inlined as an anonymous predicate inside `FormatTSVChunk`.

**New `FormatTSVChunk`:**

```go
var hasGene = F.Pipe1(
    S.IsNonEmpty,
    P.ContraMap(func(r Result) string { return r.Metadata.Gene }),
)

var toTSVRow = F.Flow2(
    func(r Result) []string { return A.From(r.Metadata.Accession, r.Metadata.Name, r.Metadata.Gene) },
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
```

`E.FromPredicate + E.Fold` is replaced by `O.FromPredicate + O.Map + O.GetOrElse` —
`Option` is the right type for absence with no error. `S.Join`, `S.Append`, `S.IsNonEmpty`,
and `P.ContraMap` replace all raw `strings.*` calls. `hasGene` and `toTSVRow` become
package-level `var` compositions instead of named functions.

**Import changes in `extract.go`:** remove `"strings"` and `E`; add `O`, `P`, `S`.

**Tests to delete:** `TestExtractRecords`, `TestToProteinRecord`, `TestHasGene`,
`TestExtractRecordsFromResponse`.

**`TestFormatTSVChunk` updated** — test setup builds `[]Result` instead of `[]ProteinRecord`.

---

## Step 4 — Refactor `fetchPageStep` → `fetchPage` in `client.go`

**Current flow:**

```
IOEither[error, APIResponse]
  →(toEither)→  Either[error, APIResponse]
  →(E.Fold)→    PageStep{ error in F1 }
```

The function synchronously executes the HTTP request and collapses success/failure into a flat
tuple. This is the original sin that forces everything downstream to re-lift.

**New:** return `IOE.IOEither[error, PageStep]` and simply `IOE.Map` the success case:

```go
func fetchPage(cfg FetchConfig) IOE.IOEither[error, PageStep] {
    return F.Pipe4(
        B.Default,
        B.WithURL(cfg.F2),
        ioehb.Requester,
        ioehttp.ReadJSON[APIResponse](cfg.F1),
        IOE.Map[error](func(resp APIResponse) PageStep {
            return T.MakeTuple2(
                FormatTSVChunk(resp.Results),
                nextURL(resp.Next),
            )
        }),
    )
}
```

`Pipe5` → `Pipe4` (one step shorter, no `toEither`, no `E.Fold`). `cfg.F1` and `cfg.F2`
accessed directly. `FormatTSVChunk(resp.Results)` calls the simplified function directly,
no `ExtractRecords` intermediary.

`toEither` itself: **keep** — it is used in `command.go:80` and `TestBuildPageRequest`. Only
remove its call inside `fetchPage`.

---

## Step 5 — Delete `pageStepEither` from `loop.go`

This function exists for one reason: to re-lift the error baked into `PageStep.F1` back into
`Either[error, PageStep]`. After Step 3, `fetchPage` returns `IOEither[error, PageStep]` where
the error is already in the right layer. The function becomes dead code and is deleted.

---

## Step 6 — Add `WriteConfig` type to `model.go`

`writePage` currently closes over the file handle via partial application
(`writePage(state.F4)`). Instead, define a config tuple that bundles the handle with the
`PageStep` so `writePage` takes a single argument and `IOE.Chain(writePage)` needs no
partial application at the call site:

```go
type WriteConfig = T.Tuple2[*os.File, PageStep]
// F1: *os.File  — file handle
// F2: PageStep  — chunk (F2.F1) and nextURL (F2.F2)
```

---

## Step 7 — Replace `writeStep` with `writePage` in `loop.go`

**Current:**

```go
func writeStep(handle *os.File) func(PageStep) E.Either[error, LoopStep] {
    return func(step PageStep) E.Either[error, LoopStep] {
        chunkResult := writeChunk(handle)(stepChunk(step))()  // IO executed here
        return E.Map[error](func([]byte) LoopStep {
            return T.MakeTuple3[error](nil, stepNext(step), "")
        })(chunkResult)
    }
}
```

Three problems: executes IO with `()` inside a supposedly pure function, returns `Either`
instead of `IOEither`, and the `LoopStep` it constructs is deleted in Step 2.

**New:** accepts a `WriteConfig` tuple, uses `IOE.TryCatchError` directly with
`(*os.File).WriteString`, and returns the nextURL from the same closure — no `writeChunk`,
no `IOE.Map`, no intermediate `[]byte`:

```go
func writePage(cfg WriteConfig) IOE.IOEither[error, string] {
    return IOE.TryCatchError(func() (string, error) {
        _, err := cfg.F1.WriteString(cfg.F2.F1)
        return cfg.F2.F2, err
    })
}
```

`cfg.F1` is the handle, `cfg.F2.F1` is the chunk, `cfg.F2.F2` is the nextURL. The write and
the return value are expressed in one `TryCatchError` with no wrapper combinators.

---

## Step 8 — Rewrite the loop body in `runLoop`

**Current annotated flow:**

```
fetchPageStep      → PageStep{ error in F1 }        (sync IO; error collapsed into tuple)
pageStepEither     → Either[error, PageStep]         (re-lifts what fetchPageStep collapsed)
E.Chain(writeStep) → Either[error, LoopStep]         (sync IO inside Either)
E.Fold(→ LoopStep) → LoopStep{ error, next, path }  (collapses back to flat)
loopNext(last)     → string                          (extracts next URL)
```

The pipeline bounces: IOEither → Either → flat → Either → flat. Four steps to do two things
(fetch, write).

**New loop body:**

An `IOE.Map` step between `fetchPage` and `IOE.Chain(writePage)` bundles the file handle from
`state.F4` together with the `PageStep` into a `WriteConfig` tuple. This lets `writePage`
receive everything through its single argument with no closure over `state`. The pipe then
continues with `toEither` and `E.Fold` to extract the next URL or surface the error:

```go
var loopErr error
currentURL = F.Pipe5(
    T.MakeTuple2(state.F1, currentURL),
    fetchPage,
    IOE.Map[error](func(step PageStep) WriteConfig {
        return T.MakeTuple2(state.F4, step)
    }),
    IOE.Chain(writePage),
    toEither[error, string],
    E.Fold(
        func(err error) string { loopErr = err; return "" },
        func(next string) string { return next },
    ),
)
if loopErr != nil {
    return "", loopErr
}
```

`Pipe4` (old) → `Pipe5` (new). The old pipe was chaos (fetch/re-lift/write/collapse);
the new one is a clean linear flow: fetch → bundle → write → run → branch. Setting
`currentURL = ""` on error terminates the `for` condition naturally as a bonus.

**Full new `runLoop`:**

```go
func runLoop(state RuntimeState) IOE.IOEither[error, string] {
    return IOE.TryCatchError(func() (string, error) {
        currentURL := state.F2

        for S.IsNonEmpty(currentURL) {
            var loopErr error
            currentURL = F.Pipe5(
                T.MakeTuple2(state.F1, currentURL),
                fetchPage,
                IOE.Map[error](func(step PageStep) WriteConfig {
                    return T.MakeTuple2(state.F4, step)
                }),
                IOE.Chain(writePage),
                toEither[error, string],
                E.Fold(
                    func(err error) string { loopErr = err; return "" },
                    func(next string) string { return next },
                ),
            )

            if loopErr != nil {
                return "", loopErr
            }
        }

        return state.F3, nil
    })
}
```

Local variables `client`, `handle`, `outputPath`, `last` are all gone. `state.F1/F2/F3/F4`
accessed directly. `"fmt"` import removed from `loop.go`.

**Fix double-wrap bug:** The old code does `fmt.Errorf("extract failed: %w", loopError(last))`
inside `runLoop`, but `command.go` also calls `wrapRunError` which does the same wrapping. The
new code returns the raw error; `command.go` wraps it once at the boundary.

---

## Step 9 — Update `TestFetchPageStep` in `interpro_test.go`

The test calls `fetchPageStep(...)` (synchronous) and asserts using `stepError`, `stepChunk`,
`stepNext`. After Steps 1 and 3 these are all gone.

**New test** (rename to `TestFetchPage`):

```go
result := fetchPage(T.MakeTuple2(ioehttp.MakeClient(server.Client()), server.URL))()

require.True(t, E.IsRight(result))
step := unwrapEither(result)
assert.Equal(t, "", step.F2)
assert.Contains(t, step.F1, "A1\tProtein 1\tgeneA")
```

---

## Complete deletion/change ledger

| File | Item | Action | Reason |
|------|------|--------|--------|
| `model.go` | `PageStep` | `Tuple4[error,string,[]ProteinRecord,string]` → `Tuple2[string,string]` | Remove error field (IOEither handles it); remove dead records field |
| `model.go` | `ProteinRecord` | **Delete** | Intermediate struct only needed by `ExtractRecords`; `FormatTSVChunk` now works on `[]Result` directly |
| `model.go` | `LoopStep` | **Delete** | Error + outputPath don't need threading; only `currentURL` (plain `string`) needed |
| `model.go` | `stepError`, `stepChunk`, `stepNext` | **Delete** | Replace with direct `.F1` / `.F2` |
| `model.go` | `loopError`, `loopNext`, `loopOutput` | **Delete** | `LoopStep` deleted |
| `extract.go` | `FormatTSVChunk` | Signature `[]ProteinRecord` → `[]Result`; replace `E.FromPredicate+E.Fold` with `O.FromPredicate+O.Map+O.GetOrElse`; use `S.Join`, `S.Append`, `P.ContraMap` | `Option` is the right type for absence; all raw `strings.*` calls replaced by fp-go combinators |
| `extract.go` | `ExtractRecords`, `toProteinRecord` | **Delete** | `FormatTSVChunk` takes `[]Result` directly; intermediate `ProteinRecord` removed |
| `extract.go` | `hasGene` | Rewritten as `var` predicate via `P.ContraMap + S.IsNonEmpty`; `toTSVRow` added as `var` flow | Composable package-level values instead of named functions |
| `client.go` | `fetchPageStep` | Rename → `fetchPage`, return `IOEither[error, PageStep]` | Stay lazy; no error-in-tuple |
| `client.go` | `toEither` call inside `fetchPage` | **Remove** (keep function itself) | No longer needed; function kept for `command.go` boundary |
| `loop.go` | `pageStepEither` | **Delete** | Existed only to reverse the flat-error collapse from old `fetchPageStep` |
| `model.go` | `WriteConfig` | **Add** `T.Tuple2[*os.File, PageStep]` | Bundle handle + PageStep so `writePage` needs no partial application |
| `loop.go` | `writeStep` | Replace → `writePage(cfg WriteConfig)` returning `IOEither[error, string]` | Stay in IOEither; single tuple argument; return nextURL not LoopStep |
| `loop.go` | `runLoop` body | `Pipe4(fetch/re-lift/write/collapse)` → `Pipe5(fetch/bundle/write/toEither/E.Fold)` | Eliminate IOEither→Either bounce; remove LoopStep; no partial application |
| `loop.go` | double-wrap `fmt.Errorf` | **Remove** | `wrapRunError` in `command.go` already wraps at the boundary |
| `loop.go` | `"fmt"` import | **Remove** | No longer used |
| `interpro_test.go` | `TestFetchPageStep` | Rename → `TestFetchPage`; update for new signature and types | Match new `fetchPage` signature and `PageStep` fields |
| `interpro_test.go` | `TestExtractRecords`, `TestToProteinRecord`, `TestHasGene`, `TestExtractRecordsFromResponse` | **Delete** | Functions deleted |
| `interpro_test.go` | `TestFormatTSVChunk` | Update setup: `[]Result` instead of `[]ProteinRecord` | Match new `FormatTSVChunk` signature |

## `nextURL` simplification in `client.go`

**Current:** triple-nested calls — `O.Fold(identity)(O.Map(deref)(O.FromNillable(next)))`.
`O.Fold` with an identity right-branch is just `O.GetOrElse`; the nesting is just a pipe.

**New:**

```go
func nextURL(next *string) string {
    return F.Pipe3(
        next,
        O.FromNillable[string],
        O.Map(func(p *string) string { return *p }),
        O.GetOrElse(F.Constant("")),
    )
}
```

`O.Fold(func() string { return "" }, func(url string) string { return url })` →
`O.GetOrElse(F.Constant(""))`. Left-to-right `F.Pipe3` replaces three levels of nesting.
Note: `O.FromNillable[A](a *A) Option[*A]` wraps without dereferencing, so the `O.Map` deref
step is still required.

---

## What does not change

- `command.go` — no references to any deleted type or function
- `extract.go` — **changed**: `FormatTSVChunk` simplified; `ExtractRecords`, `toProteinRecord`, `hasGene` deleted
- `tsv.go` — unchanged
- `runtimeHandle` in `model.go` — keep; used in `command.go`
- `toEither` in `client.go` — keep; used in `command.go:80` and `TestBuildPageRequest`
- `nextURL` in `client.go` — keep; used in new `fetchPage`
- All other tests — unchanged
