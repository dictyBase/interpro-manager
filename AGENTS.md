# AGENTS.md


## Essential Commands

```bash
# Test
gotestsum --format pkgname-and-test-fails --format-hide-empty-pkg -- ./...

# Test (verbose)
gotestsum --format testdox --format-hide-empty-pkg -- ./...

# Watch mode
gotestsum --watch --format pkgname-and-test-fails --format-hide-empty-pkg -- ./...

# Lint
golangci-lint run ./...

# Format
golangci-lint fmt

# Run
go run ./cmd/container-cli/... build --help
```

---

## Dependencies

| Package | Version | Role |
|---|---|---|
| `github.com/urfave/cli/v3` | v3.6.2 | CLI framework |
| `github.com/IBM/fp-go/v2` | v2.2.6 | Functional programming |
| `github.com/stretchr/testify` | v1.11.1 | Test assertions |

---


## Functional Programming Conventions

### Core rules

- **No imperative branching** — use `E.Fold`, `E.FromPredicate`, `E.Chain`
- **Side effects isolated in IOEither** — `exec.CommandContext` receives direct argv slice, never shell strings
- **Validation is pure** — separate from process execution
- **Use `F.Pipe1/2/3/etc`** — match arity to transform count

### Key fp-go combinators

| Combinator | Purpose |
|---|---|
| `E.FromPredicate(pred, errFn)` | Lift value into Either based on predicate |
| `E.Chain` | Sequence dependent Either operations |
| `E.Fold(onLeft, onRight)` | Terminal branching at boundary |
| `E.MapTo[E, A](b)` | Replace Right value |
| `IOE.FromEither` | Lift Either into IOEither |
| `IOE.Chain` | Sequence IOEither operations |
| `IOE.WithResource` | Acquire/use/release pattern |
| `IOE.TryCatchError(func() (A, error))` | Wrap fallible effect |
| `A.Chain` | FlatMap over arrays |
| `A.Flatten` | Flatten `[][]string` to `[]string` |
| `R.FromEntries` | Build Record from key-value pairs |
| `R.Lookup` | Lookup value in Record |
| `P.MakePair` | Create Pair for Record entries |
| `F.Void` | Empty struct for side-effect-only results |

### Import aliases

| Alias | Package |
|---|---|
| `E` | `either` |
| `IOE` | `ioeither` |
| `IOEF` | `ioeither/file` |
| `A` | `array` |
| `F` | `function` |
| `O` | `option` |
| `P` | `pair` |
| `R` | `record` |
| `S` / `Str` | `string` |


## Testing Conventions

- Tests live alongside source in `internal/containerbuild/`
- Pure functions tested directly — no mocking needed
- Run tests after every modification: `gotestsum --format pkgname-and-test-fails --format-hide-empty-pkg -- ./...`


## Gotchas

- **Build context is always `.`** — never exposed to users
- **`urfave/cli/v3`**: use `cmd.String("flag")`, `cmd.StringSlice("flag")`, `cmd.Bool("flag")` inside Action
- **fp-go generics**: type parameters often required explicitly (e.g., `E.MapTo[error, string](true)`)
- **`IOEither` is lazy**: must call `program()` to trigger execution
- **Use `E.Fold` for terminal branching**, not `if` statements in application code
