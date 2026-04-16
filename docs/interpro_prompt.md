# InterPro CLI Development Prompt

Construct a Golang CLI application for interacting with the InterPro protein database, adhering to strict functional programming principles.

## Objective
Implement a subcommand that extracts protein metadata for *Dictyostelium discoideum* (Taxonomy ID: 44689) and saves it to a TSV file.

## Functional Requirements
1.  **Data Source**: Query the InterPro API at: `https://www.ebi.ac.uk/interpro/api/protein/UniProt/taxonomy/uniprot/44689/?page_size=20`
2.  **Pagination**: Implement recursive iteration by following the `next` URL in the JSON response until it is null.
3.  **Data Extraction**: From each result in `results[]`, extract:
    *   `metadata.accession` (UniProt ID)
    *   `metadata.name` (Protein Name)
    *   `metadata.gene` (Gene ID/Name)
4.  **Filtering**: Skip entries where the `gene` field is missing or empty.
5.  **Output**: Write the collected records to a TSV file.

## Technical Architecture
1.  **Framework**: Use `urfave/cli/v3`.
2.  **Paradigm**: Pure functional style using `fp-go/v2`.
3.  **Core Libraries**:
    *   `github.com/IBM/fp-go/v2`
    *   Utilize the `effect` package for managed side effects.
    *   Mix with `IOEither`, `Either`, `Option`, `Predicate`, `Eq`, and `Array` packages.

## File & Path Operations — Mandatory fp-go v2 File API

All file and path operations **must** use the fp-go v2 file packages. No direct `os` or `filepath` stdlib calls for file I/O or path manipulation.

### Pure Path Composition (`github.com/IBM/fp-go/v2/file`)

Use the `file` package (aliased as `File`) for all path construction:

```go
import File "github.com/IBM/fp-go/v2/file"
import F "github.com/IBM/fp-go/v2/function"

// Build reusable path appenders
var addOutput = File.Join("output.tsv")

// Compose multi-segment path builders
var toOutputDir = F.Flow3(
    File.Join("data"),
    File.Join("interpro"),
    File.Join("output.tsv"),
)
// toOutputDir("/tmp") == "/tmp/data/interpro/output.tsv"
```

### Effectful File I/O (`github.com/IBM/fp-go/v2/ioeither/file`)

Use the `ioeither/file` package (aliased as `FILE`) for all file side effects:

```go
import FILE "github.com/IBM/fp-go/v2/ioeither/file"
import IOE "github.com/IBM/fp-go/v2/ioeither"
import F "github.com/IBM/fp-go/v2/function"
```

| Operation | Function | Signature |
|-----------|----------|-----------|
| **Write file (simple)** | `FILE.WriteFile(path, perm)` | `[]byte -> IOEither[error, []byte]` |
| **Write file (bracket)** | `FILE.WriteAll[*os.File](data)` | `IOEither[error, *os.File] -> IOEither[error, []byte]` |
| **Read file** | `FILE.ReadFile(path)` | `IOEither[error, []byte]` |
| **Create directory** | `FILE.MkdirAll(path, perm)` | `IOEither[error, string]` |
| **Open / Create** | `FILE.Open(path)` / `FILE.Create(path)` | `IOEither[error, *os.File]` |
| **Remove** | `FILE.Remove(path)` | `IOEither[error, string]` |

### Example: MkdirAll → derive path → WriteFile pipeline

```go
program := F.Pipe3(
    FILE.MkdirAll("output/interpro", 0o755),
    IOE.Map[error](File.Join("proteins.tsv")),
    IOE.Chain(func(path string) IOE.IOEither[error, []byte] {
        return F.Pipe1(
            []byte("accession\tgene\tname\n"),
            FILE.WriteFile(path, 0o644),
        )
    }),
)
```

### Example: WriteAll (bracket pattern — resource-safe)

```go
writeAllTo := F.Curry2(
    func(path string, data []byte) IOE.IOEither[error, []byte] {
        return F.Pipe1(
            FILE.Create(path),
            FILE.WriteAll[*os.File](data),
        )
    },
)
```

## No Imperative Control Flow

Imperative `if` statements are **forbidden** in application code. Use fp-go combinators instead:

| Imperative | Functional Replacement |
|------------|----------------------|
| `if cond { a } else { b }` | `E.Fold(onLeft, onRight)` or `O.Fold(onNone, onSome)` |
| `if err != nil { return err }` | `E.Chain`, `IOE.Chain` |
| `if x == "" { skip }` | `E.FromPredicate(pred, errFn)` or `A.Filter` |
| `if ok { doThing }` | `O.Map`, `O.Fold` |
| `switch/case` | `F.Switch` or `P.Any`/`P.All` combinators |
| `for range xs { ... }` | `A.Map`, `A.Filter`, `A.Chain`, `A.Reduce` |

Terminal branching at the program boundary uses `E.Fold` (or `IOE.Fold`). Internal logic stays in the `Either`/`Option` monad.

## Implementation Patterns
-   Refer to `fp-go-concepts/v2/http` for HTTP/JSON handling.
-   Refer to `fp-go-concepts/v2/file` for all file/path operations (see examples above).
-   Refer to `modware-import` (specifically the `gpad` subcommand) for streaming/paginated download patterns.
-   Architect the solution following the modularity and structure demonstrated in `crush-sandbox`.

## Side Effect Isolation
All non-pure operations (network, file I/O) must be encapsulated within `IOEither`. Use `F.Pipe` for all transformations. No imperative `if` branching anywhere in the codebase.

## Reference Data
The API response structure is defined in `docs/interpro_api_response.json`. Ensure the JSON unmarshaling logic matches this schema precisely.
