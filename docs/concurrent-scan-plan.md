# Concurrent Scan Subcommand — Architecture & Implementation Plan

> **Status:** Draft — for review and discussion  
> **Target:** `scan` subcommand of `interpro-manager`  
> **Goal:** Process FASTA records concurrently (max 25 in-flight) through the InterProScan submit→poll→download→save pipeline


---

## 1. Current Architecture Summary

### 1.1 The Scan Pipeline (Sequential)

```
CLI flags (fasta, email, output, seq-type, poll-interval, timeout)
  │
  ├─ extractScanRequest      → ScanRequest struct
  ├─ validateScanRequest     → Either[error, ScanRequest]
  │   ├─ validate email (format + parse)
  │   ├─ validate fasta path
  │   └─ stat the file to confirm it exists
  │
  ├─ IOE.FromEither          → lift into IOEither
  ├─ IOE.Map(makeHTTPClient) → bundle http.Client + ScanRequest into SubmitArgs
  ├─ IOE.ChainFirst(mkdir)   → ensure output dir exists
  │
  └─ streamFastaRecords      → IOEither[error, []string]
      │
      └─ for each Fasta record (sequential, via channel):
          │
          ├─ IOE.FromEither(record)     → lift parsed record
          ├─ IOE.Map(to SubmitInput)    → bundle client + config + record
          ├─ IOE.Chain(buildSubmit)     → POST /run → SubmittedJob
          ├─ IOE.Chain(pollJob)         → GET /status until FINISHED → CompletedJob
          ├─ IOE.Chain(downloadAndSave) → GET /result + write JSON file → string (path)
          ├─ toEither                   → execute the IOEither
          └─ E.Fold                    → collect path or capture error
      │
      └─ first error aborts the stream; returns all collected paths otherwise
```

### 1.2 Files involved in the scan subcommand

| File | Purpose |
|------|---------|
| `cmd/interpro-cli/main.go:51-90` | CLI flag definitions, wires `interpro.Scan` as Action |
| `internal/interpro/scan.go` | Top-level `Scan` entry point, validation, orchestration |
| `internal/interpro/scan_model.go` | Type definitions: `ScanRequest`, `SubmittedJob`, `CompletedJob`, `SubmitArgs`, `SubmitInput` |
| `internal/interpro/scan_loop.go` | `streamFastaRecords` — sequential loop driver |
| `internal/interpro/scan_submit.go` | `buildSubmitRequester` — POST /run → `SubmittedJob` |
| `internal/interpro/scan_poll.go` | `pollJob`, `tickOnce`, `buildStatusHandler`, `getJobStatus` — polling loop |
| `internal/interpro/scan_result.go` | `downloadAndSave`, `saveResult` — GET /result + file write |
| `internal/interpro/extract.go:41-46` | `extractSeqID` — extracts sequence ID from FASTA header |
| `internal/interpro/client.go:14-16` | `toEither` — executes an IOEither lazily |
| `internal/seqio/fasta.go` | `ParseFASTA` — lazy FASTA parser returning `SeqResult[Fasta]` channel |

### 1.3 Key Type Definitions

```go
// From scan_model.go
type ScanRequest struct {
    FastaPath    string
    Email        string
    OutputDir    string
    SeqType      string
    BaseURL      string
    PollInterval time.Duration
    Timeout      time.Duration
}

type SubmittedJob struct {
    JobID  string
    SeqID  string
    Client ioehttp.Client
    Config ScanRequest
}

type CompletedJob struct {
    JobID  string
    SeqID  string
    Client ioehttp.Client
    Config ScanRequest
}

type SubmitArgs  = T.Tuple2[ioehttp.Client, ScanRequest]
type SubmitInput = T.Tuple3[ioehttp.Client, ScanRequest, seqio.Fasta]
```

### 1.4 The Per-Record Functional Pipeline (what we want to reuse)

The core processing for a single record is a clean IOEither pipeline:

```
seqio.Fasta record
  → buildSubmitRequester(SubmitInput)   : IOEither[error, SubmittedJob]
  → pollJob(SubmittedJob)               : IOEither[error, CompletedJob]
  → downloadAndSave(CompletedJob)       : IOEither[error, string]  // path to saved JSON
```

This pipeline is **already correct, well-tested, and purely functional**. The concurrency layer should **wrap** this pipeline, not modify it.

### 1.5 The FASTA Parser

`ParseFASTA(path)` returns `IR.SeqResult[Fasta]` — a channel-based lazy iterator. Each element is `Either[error, Fasta]`. The file is lazily read and closed when iteration completes. The channel **must be consumed from a single goroutine** (Go channels are not multi-consumer).


---

## 2. Problem Statement

The current `streamFastaRecords` processes records **one at a time,
sequentially**. For a FASTA file with N records:

- Submission of record `i+1` waits for submission, polling, and download of record `i` to complete.
- Each record spends most of its time **waiting** — polling an HTTP endpoint until the server-side analysis finishes.
- Total runtime ≈ N × (submit_time + poll_wait_time + download_time).

For example, if polling takes 5 minutes per job and there are 100 records:
- Sequential: ~500 minutes (8+ hours)
- Concurrent (25 at a time in 4 batches): ~20 minutes (4 × 5 minutes)

The interproscan REST API supports concurrent job submissions — there is no
server-side restriction preventing parallel requests. The only constraints are
client-side: API rate limits, network bandwidth, and local resources.

### 2.1 Key Concurrency Opportunities

| Phase | Concurrency Opportunity | Blocking? |
|-------|------------------------|-----------|
| **Submit** (`POST /run`) | High — each submission is an independent HTTP POST. Can send all 25 at once. | Short (seconds) |
| **Poll** (`GET /status`) | High — each job polls independently. Most time is waiting for server-side processing. | Long (minutes) |
| **Download** (`GET /result`) | Medium — each download is an independent HTTP GET. | Short (seconds) |
| **Save** (write JSON file) | Medium — independent file writes to separate files. | Short (milliseconds) |


---

## 3. Design Goals & Constraints

### 3.1 Non-negotiable Constraints

1. **Preserve the per-record functional pipeline.** `buildSubmitRequester → pollJob → downloadAndSave` remains untouched as a pure IOEither composition.
2. **New subcommand `concurrent-scan`.** A separate subcommand with its own flag set. Does not modify the existing `scan` subcommand.
3. **Preserve the existing error model.** Errors are propagated through the `Either[error, A]` / `IOEither[error, A]` monad stack.
4. **Max 25 concurrent in-flight jobs.** This is the API's informal guidance (or configurable).
5. **Each record saves to its own JSON file.** Same file naming convention: `{SeqID}_{JobID}.json`.
6. **fp-go everywhere.** The per-record pipeline stays in fp-go. The orchestration may use Go primitives but should present an fp-go-compatible interface.
7. **Stream the FASTA file — never slurp it into memory.** The parser (`ParseFASTA`) is a channel-based lazy iterator. Records must be consumed as they are parsed, not collected into a `[]seqio.Fasta` slice. FASTA files can contain tens of thousands of sequences (genome-scale); loading them into memory upfront is unacceptable.

### 3.2 Design Goals

1. **Minimal refactoring.** The submission, polling, download, and save functions should not change at all.
2. **Clear separation of concerns.** The concurrency orchestration lives in its own file, separate from the pure functional pipeline.
3. **Composable error handling.** Errors should compose: per-record errors should be collectable or fail-fast, depending on configuration.
4. **Deterministic file output.** Each file path depends on `SeqID + JobID`, which are inherently unique — no race conditions on file writes.
5. **Testable in isolation.** The concurrency orchestrator should be testable with a mock HTTP server, just like the existing tests.

### 3.3 Open Design Questions (for you to answer)

| # | Question | Options |
|---|----------|---------|
| **Q1** | Should concurrency limit be a CLI flag (`--concurrency`)? | Default 25, configurable, or hard-coded 25 |
| **Q2** | Error handling: fail-fast (first error cancels all) or best-effort (collect all errors, report at end)? | Fail-fast is simpler and matches current sequential behavior. Best-effort is more robust for large batches. |
| **Q3** | Result ordering: should output paths be reported in FASTA order, or is any order acceptable? | Any order is simpler (no ordering overhead). FASTA order requires sorting/buffering. |
| **Q4** | New subcommand (`concurrent-scan`) or replace existing `scan`? | New subcommand allows side-by-side comparison. Replacing `scan` is cleaner long-term. |
| **Q5** | Should an aggregated summary be printed (e.g., "25/25 succeeded", "3 failed")? | Yes, this is valuable for large batches. Current sequential version just prints each file path. |


---

## 4. Detailed Approach Analysis

### 4.1 Approach A: `TraverseArrayPar` with Batch Chunking — ❌ ELIMINATED

> **Eliminated** because `TraverseArrayPar` requires a `[]seqio.Fasta` slice — it cannot consume a channel-based iterator. This forces slurp-first semantics, which violates constraint #7.
>
> Additional issues: no concurrency bound (all 25 launch simultaneously within a batch), no context cancellation, and all-or-nothing batch error semantics. Even if the FASTA file is small, these deeper limitations make it unsuitable for production use.

---

### 4.2 Approach B: `TraverseArrayPar` with Semaphore — ❌ ELIMINATED

> **Eliminated** for the same reason as Approach A: `TraverseArrayPar` requires slurping the FASTA file into a `[]seqio.Fasta` slice. The semaphore addresses the concurrency-bounding problem but does not change the fundamental requirement that all records be loaded into memory before processing begins.

---

### 4.3 Approach C: Semaphore-Bounded Worker Pool (SELECTED)

**Core idea:** Use a `semaphore.Weighted` for bounded concurrency (max 25 in-flight) with Go's standard `sync.WaitGroup` for worker coordination. Each worker runs the functional IOEither pipeline for one record. Errors are collected per-record (best-effort mode) rather than cancelling on first failure.

**How it works:**
1. A single goroutine reads FASTA records from the channel and dispatches tasks to workers.
2. Each dispatch acquires a semaphore token (blocks if 25 workers are in-flight), then launches a goroutine via `sync.WaitGroup`.
3. Each worker processes one record through the existing IOEither pipeline, releases the semaphore on completion.
4. Per-record errors are collected into a mutex-protected error slice (best-effort — processing continues after individual failures).
5. After all workers finish, results and errors are aggregated into a summary.

**Pseudo-design (not actual code):**

```
// New file: internal/interpro/scan_concurrent.go

func streamFastaRecordsConcurrent(args SubmitArgs) IOE.IOEither[error, []string] {
    return IOE.TryCatchError(func() ([]string, error) {
        sem := semaphore.NewWeighted(int64(args.F2.Concurrency))

        var wg sync.WaitGroup
        var mu sync.Mutex
        var results []string
        var errors []string

        // Reader loop: dispatch FASTA records to workers
        for record := range seqio.ParseFASTA(args.F2.FastaPath) {
            // Handle parse error — per-record, continue to next
            if record is Left:
                mu.Lock()
                errors = append(errors, formatParseError(record.Err))
                mu.Unlock()
                continue

            rec := record.Right

            // Block if N workers are already running
            if err := sem.Acquire(context.Background(), 1); err != nil {
                return nil, err
            }

            wg.Add(1)
            go func(r seqio.Fasta) {
                defer sem.Release(1)
                defer wg.Done()

                // Run the functional pipeline
                result := F.Pipe4(
                    T.MakeTuple3(args.F1, args.F2, r),
                    buildSubmitRequester,
                    IOE.Chain(pollJob),
                    IOE.Chain(downloadAndSave),
                    toEither[error, string],
                )

                E.Fold(
                    func(err error) {
                        mu.Lock()
                        errors = append(errors,
                            fmt.Sprintf("seq %s: %v", extractSeqID(r), err))
                        mu.Unlock()
                    },
                    func(path string) {
                        mu.Lock()
                        results = append(results, path)
                        mu.Unlock()
                    },
                )(result)
            }(rec)
        }

        // Wait for all workers to finish
        wg.Wait()

        // Return aggregated results
        return results, fmt.Errorf(join errors if any)
    })
}
```

**Pros:**
- ✓ Bounded concurrency via semaphore
- ✓ Per-record error collection (best-effort) — a single failure doesn't abort other in-flight work
- ✓ Each worker runs the exact same functional IOEither pipeline — zero changes to submit/poll/download/save
- ✓ Results collected in a thread-safe way
- ✓ Clear separation: the orchestrator (`scan_concurrent.go`) is imperative Go; the per-record processing is pure fp-go
- ✓ Streams FASTA records (no need to slurp entire file)
- ✓ Well-established Go pattern, easy to understand and maintain

**Cons:**
- ✗ Mixes Go concurrency primitives with fp-go (but this is intentional and bounded)
- ✗ More imperative code than a pure fp-go approach
- ✗ Result ordering is non-deterministic (depends on which jobs finish first)
- ✗ No built-in cancellation — once dispatched, a worker runs to completion even if many others fail (acceptable for best-effort mode)

**Best for:** Production use, large FASTA files, when you need robust per-record error handling.

---

### 4.4 Approach D: Channel Fan-Out / Fan-In (PURE GO)

**Core idea:** Classic Go concurrency pattern. One producer goroutine sends FASTA records into a channel. N worker goroutines consume from the channel. Results are collected via a result channel.

**How it works:**
1. Create a buffered channel for FASTA records (acts as a work queue).
2. Launch 25 worker goroutines that read from the channel and process records.
3. Each worker runs the IOEither pipeline for its record.
4. Workers send results (path or error) to a result channel.
5. A collector goroutine aggregates results from the result channel.

**Pseudo-design (not actual code):**

```
records := make(chan seqio.Fasta, 25)     // work queue
results := make(chan result, 25)           // result collector

// Producer: read FASTA, feed into channel
go func() {
    for r := range ParseFASTA(path) { records <- r }
    close(records)
}()

// Workers: 25 goroutines consuming from records channel
var wg sync.WaitGroup
for i := 0; i < 25; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        for rec := range records {
            // run pipeline, send to results channel
        }
    }()
}

// Closer: wait for workers, close results channel
go func() { wg.Wait(); close(results) }()

// Collector: aggregate results
for r := range results { ... }
```

**Pros:**
- ✓ Classic, well-understood pattern
- ✓ Natural backpressure via buffered channels
- ✓ Streaming — no need to slurp entire FASTA file

**Cons:**
- ✗ More boilerplate than errgroup
- ✗ Error handling is manual — no built-in cancellation on first error
- ✗ Worker lifecycle management is explicit and verbose
- ✗ No built-in context propagation
- ✗ Harder to reason about than errgroup (multiple channels, goroutines, WaitGroups)

**Best for:** When you want maximum control over the concurrency model, or when errgroup is not available.

---

### 4.5 Approach E: ReaderIOResult + TraverseArray — ❌ ELIMINATED

> **Eliminated.** `RIO.TraverseArray` has the same structural requirement as `TraverseArrayPar`: it operates on `[]A` slices, not on lazy iterators. Converting the entire HTTP layer to `ReaderIOResult` is a major refactoring that still doesn't solve the streaming problem — it would still require slurping. The combination of high effort + no streaming makes this non-viable.
>
> **Original idea (retained for reference):** Convert the entire HTTP layer from `IOEither` to `ReaderIOResult` (which carries `context.Context`). Then use `RIO.TraverseArray` for parallel execution with context cancellation support.
>
> **Original pros/cons:**
> - ✓ Most idiomatic fp-go approach for HTTP with context
> - ✓ Context propagation is built into the type
> - ✗ Requires rewriting the entire HTTP layer (submit, poll, download)
> - ✗ Major refactoring — high risk, high effort
> - ✗ Still requires `[]A` — no streaming support
>
> **Best for:** (Not recommended) — only viable if the project plans to adopt `ReaderIOResult`/`Effect` more broadly AND the FASTA file is guaranteed small.

---

### 4.7 Comparison Matrix (Viable Approaches Only)

| Criterion | C: Semaphore Worker Pool | D: Channel Fan-out |
|---|---|:---:|:---:|
| Functional purity | ★★★☆☆ | ★★☆☆☆ |
| Bounded concurrency | ★★★★★ | ★★★★★ |
| Error handling | Per-record (best-effort) | Manual |
| Streaming FASTA | ✓ | ✓ |
| Reuses existing pipeline | ✓ | ✓ |
| Code changes required | Low | Low |
| Test complexity | Medium | Medium |
| Production readiness | ★ Best | Good |

> **Approaches A, B, and E eliminated** — all three require slurping the FASTA file into memory (see §4.1, §4.2, §4.5).


---

## 5. Error Handling Architecture

### 5.1 Error Taxonomy

In the concurrent scan pipeline, errors can originate from multiple sources:

| Error Source | Type | Example |
|-------------|------|---------|
| **FASTA parse error** | `Left(error)` from `ParseFASTA` | Malformed FASTA, stray sequence data |
| **Submission failure** | HTTP error from `buildSubmitRequester` | 500 from API, network timeout, invalid sequence |
| **Poll timeout** | Timeout error from `pollJob` | Job runs longer than `--timeout` |
| **Poll error status** | Status error from `pollJob` | API returns `FAILED`, `NOT_FOUND`, etc. |
| **Download failure** | HTTP error from `downloadAndSave` | 500 from result endpoint, network error |
| **File write failure** | IO error from `saveResult` | Disk full, permission denied |
| **Concurrency error** | Error from semaphore | Semaphore acquisition failed |

### 5.2 Error Handling Strategies

#### Strategy A: Best-Effort / Collect-All (Default)

**Behavior:** All records are attempted. Per-record errors are collected. Successful results are returned alongside a list of failures.

```
Record 1: ✓ saved → "/out/seq1_JOB1.json"
Record 2: ✗ submit error → "seq2: 500 Internal Server Error"
Record 3: ✓ saved → "/out/seq3_JOB2.json"
Record 4: ✗ poll timeout → "seq4: timed out"

Result: Right({
    Paths: ["/out/seq1_JOB1.json", "/out/seq3_JOB2.json"],
    Errors: ["seq2: 500 Internal Server Error", "seq4: timed out"]
})
```

**Implementation:**
- Use `sync.WaitGroup` for worker coordination (no cancellation on error).
- Each worker runs to completion, recording its own outcome.
- A mutex-protected error slice collects per-record failures.
- After all workers finish, aggregate results and errors into a summary report.
- FASTA parse errors also recorded per-record (skip the malformed record, continue).

**When to use:** This is the default and only mode. Large batches benefit from not wasting in-flight work on a single failure.

---

### 5.3 Error Message Design

Per-record errors should be prefixed with the sequence ID for traceability:

```
seq tr|Q95Q25|ANMK_CAEEL: submission failed: 500 Internal Server Error
seq sp|P12345|PROT2: job timed out after 30m0s
seq tr|Q9XYZ1: API returned status FAILED
```

The `extractSeqID` function already exists in `extract.go:41-46` and extracts the identifier token from the FASTA header.

### 5.4 Reporting

The concurrent version should produce a summary at the end:

```
--- Scan Complete ---
Total records:  100
Successful:     97
Failed:         3
  seq ABC: timed out
  seq DEF: 500 Internal Server Error
  seq GHI: API returned status FAILED
Output files written to: ./output/
```

The existing `reportScanResults` function should be upgraded to handle this summary format.


---

## 6. Mixing Go Concurrency with fp-go: Design Principles

### 6.1 The Boundary Contract

The concurrency layer and the functional layer meet at a well-defined boundary:

```
┌─────────────────────────────────────────────────────────┐
│  CONCURRENCY ORCHESTRATOR (imperative Go)               │
│                                                         │
│  - semaphore / WaitGroup                               │
│  - worker lifecycle                                     │
│  - result aggregation (mutex-protected slice)           │
│                                                         │
│  Calls into:  processOneRecord(SubmitInput) → string    │
│               (executes the IOEither pipeline)          │
├─────────────────────────────────────────────────────────┤
│  PER-RECORD PIPELINE (functional fp-go)                 │
│                                                         │
│  - buildSubmitRequester                                 │
│  - pollJob                                              │
│  - downloadAndSave                                      │
│                                                         │
│  All pure composition, no concurrency awareness         │
└─────────────────────────────────────────────────────────┘
```

### 6.2 Functional Wrapper for Single-Record Processing

Introduce a new function that wraps the existing pipeline into a single callable unit:

```go
// processOneRecord executes the full submit→poll→download→save pipeline
// for a single FASTA record.
//
// Type: SubmitInput → E.Either[error, string]
//
// This is NOT a new implementation — it composes the existing functions.
func processOneRecord(input SubmitInput) E.Either[error, string] {
    return F.Pipe4(
        input,
        buildSubmitRequester,     // SubmitInput       → IOEither[error, SubmittedJob]
        IOE.Chain(pollJob),       // SubmittedJob      → IOEither[error, CompletedJob]
        IOE.Chain(downloadAndSave), // CompletedJob    → IOEither[error, string]
        toEither[error, string],  // IOEither → Either (execute lazily)
    )
}
```

This function:
- Is pure composition — it just pipes existing functions together.
- Is testable independently (see existing `TestProcessOneFastaIntegration`).
- Is the single entry point that the concurrency layer calls.
- Returns `Either[error, string]` — the concurrency layer folds over this.

### 6.3 Polling Timeout (Unchanged)

The current `pollJob` creates its own `context.Background()` with a per-job timeout via `context.WithTimeout`. Since best-effort mode does not cancel in-flight workers on error, there is no need for a shared parent context. Each worker's `pollJob` runs with its own independent timeout, matching the existing behavior exactly.

**No change to `pollJob` signature required.** The existing `pollJob(job SubmittedJob) IOE.IOEither[error, CompletedJob]` is kept as-is.

### 6.4 Imperative Residue Principle

Following the pattern established in the `pollJob` refactoring plan (where the `select` block is acknowledged as "the minimal imperative residue"):

> **Go has no monadic primitive for goroutine orchestration with bounded concurrency. The semaphore + WaitGroup block is the minimal imperative residue — all per-record logic above it is fully functional.**

This is the same principle that justified the `select` block in `pollJob`. The imperative shell is thin, bounded, and intentional.

### 6.5 Why Not IOEither.TraverseArrayPar?

`TraverseArrayPar` is the most functional approach, but has three critical limitations:

1. **Requires slurping.** `TraverseArrayPar` operates on `[]A` slices — it cannot consume a lazy channel-based iterator. This alone disqualifies it given the non-negotiable streaming constraint (§3.1, #7).
2. **No concurrency bounding.** For 25 records this is fine. For 1000 records it launches 1000 goroutines — potentially problematic for memory and API rate limits.
3. **All-or-nothing batch semantics.** If any record in a `TraverseArrayPar` batch fails, the entire batch returns `Left`. No partial results — incompatible with the best-effort error strategy.

Approach C (semaphore + WaitGroup) solves all three while keeping the functional pipeline intact: streams the FASTA file, bounds concurrency precisely, and collects per-record errors.

### 6.6 The Argument for Mixed Style

Some functional programming purists may object to mixing goroutines with fp-go. The counter-argument:

- **fp-go itself uses goroutines internally.** `ApPar` (used by `TraverseArrayPar`) launches goroutines. The `io` package's parallel operations are built on goroutines.
- **Go's concurrency model is first-class.** Goroutines and channels are Go's native concurrency primitives, just like Haskell has `par` and `async`. Using them alongside fp-go is not a violation — it's working with the language, not against it.
- **The project already accepts this.** `pollJob` has a `select` block and `TryCatchError` wraps an imperative `for` loop. The project's philosophy is: "keep the imperative shell thin."
- **The boundary is clear.** The imperative code is in the orchestrator; the functional code is in the per-record pipeline. They don't leak into each other.


---

## 7. Recommended Design: Approach C (Semaphore-Bounded Worker Pool)

### 7.1 New Subcommand

A new `concurrent-scan` subcommand is added to `cmd/interpro-cli/main.go`, separate from the existing `scan` subcommand. It shares the same scan-related flags plus one new flag:

```go
{
    Name:  "concurrent-scan",
    Usage: "Submit protein sequences to InterProScan concurrently and save JSON results",
    Flags: []cli.Flag{
        &cli.StringFlag{Name: "fasta", Aliases: []string{"f"}, Usage: "Path to FASTA file"},
        &cli.StringFlag{Name: "email", Aliases: []string{"e"}, Sources: cli.EnvVars("EBI_EMAIL"), Usage: "Email for EMBL-EBI Job Dispatcher (required)"},
        &cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: ".", Usage: "Output directory for JSON results"},
        &cli.StringFlag{Name: "seq-type", Aliases: []string{"s"}, Value: "p", Usage: "Sequence type: p (protein) or n (nucleotide)"},
        &cli.DurationFlag{Name: "poll-interval", Value: 15 * time.Second, Usage: "How often to check job status"},
        &cli.DurationFlag{Name: "timeout", Value: 30 * time.Minute, Usage: "Maximum time to wait for a single job"},
        &cli.IntFlag{Name: "concurrency", Aliases: []string{"c"}, Value: 25, Usage: "Maximum number of concurrent InterProScan jobs"},
    },
    Action: interpro.ConcurrentScan,
}
```

### 7.2 New Types

```go
// Added to scan_model.go

// ScanRequest extended with concurrency setting.
type ScanRequest struct {
    // ... existing fields ...
    Concurrency int  // NEW: max concurrent in-flight jobs (default 25)
}
```

### 7.3 New Functions

| Function | File | Purpose |
|----------|------|---------|
| `processOneRecord` | `scan_concurrent.go` | Composes submit→poll→download→save for one record into a single `Either[error, string]` |
| `streamFastaRecordsConcurrent` | `scan_concurrent.go` | Concurrent orchestrator: reads FASTA, dispatches workers via WaitGroup, collects results |
| `ConcurrentScan` | `scan_concurrent.go` | Top-level entry point for the `concurrent-scan` subcommand |
| `reportConcurrentResults` | `scan_concurrent.go` | Reporter: prints summary with success/failure counts |

### 7.4 Modified Functions

| Function | Change | Reason |
|----------|--------|--------|
| `ConcurrentScan` | NEW entry point | Separate entry for concurrent-scan subcommand |
| `ScanRequest` | Add `Concurrency int` field | Store concurrency setting |
| `extractScanRequest` | Read `Concurrency` from CLI flag | Wire the flag |

No existing pipeline functions are modified. `pollJob`, `buildSubmitRequester`, `downloadAndSave`, and all other per-record functions remain untouched.

### 7.5 File Layout

```
internal/interpro/
  ├─ scan.go              ← Unchanged: sequential scan subcommand
  ├─ scan_model.go        ← Modified: Concurrency field added to ScanRequest; new types
  ├─ scan_concurrent.go   ← NEW: top-level ConcurrentScan entry + orchestrator + worker
  ├─ scan_loop.go         ← Unchanged: sequential fallback
  ├─ scan_submit.go       ← Unchanged: per-record functional pipeline
  ├─ scan_poll.go         ← Unchanged: pollJob
  ├─ scan_result.go       ← Unchanged: per-record functional pipeline
  ├─ extract.go           ← Unchanged: extractSeqID
  └─ client.go            ← Unchanged: toEither
```

### 7.6 Top-Level Control Flow

```
ConcurrentScan(ctx, cmd)   ← NEW entry point for concurrent-scan subcommand
  │
  ├─ extractScanRequest(cmd)           → ScanRequest (includes Concurrency)
  ├─ validateScanRequest(req)          → Either[error, ScanRequest]
  ├─ IOE.FromEither
  ├─ IOE.Map(makeSubmitArgs)           → SubmitArgs
  ├─ IOE.ChainFirst(ensureOutputDir)
  │
  └─ streamFastaRecordsConcurrent(args)  → IOEither[error, []string]  (NEW)
  │
  └─ toEither → E.Fold(wrapScanError, reportConcurrentResults)
```

The existing `Scan` entry point and `streamFastaRecords` are left untouched for the sequential `scan` subcommand.

### 7.7 Concurrent Orchestrator Design

```
streamFastaRecordsConcurrent(args SubmitArgs) IOE.IOEither[error, []string]

1. Parse CLI config:
   - concurrency limit from args.F2.Concurrency (default 25)
   - output dir, client, etc. from args

2. Create synchronization primitives:
   sem := semaphore.NewWeighted(int64(concurrency))
   var wg sync.WaitGroup

3. Create shared state:
   var mu sync.Mutex
   var results []string
   var errors []string

4. Reader loop (runs in calling goroutine, not a separate one):
   for record := range seqio.ParseFASTA(fastaPath):
     │
     ├─ If Left(err): record parse error in errors slice, continue
     │
     └─ If Right(rec):
        sem.Acquire(context.Background(), 1)   // block if N workers running
        │
        wg.Add(1)
        go func(r seqio.Fasta) {               // launch worker goroutine
            defer sem.Release(1)
            defer wg.Done()

            input := T.MakeTuple3(args.F1, args.F2, r)

            // Execute per-record pipeline
            result := processOneRecord(input)

            E.Fold(
                func(err error) {
                    mu.Lock()
                    errors = append(errors,
                        fmt.Sprintf("seq %s: %v", extractSeqID(r), err))
                    mu.Unlock()
                },
                func(path string) {
                    mu.Lock()
                    results = append(results, path)
                    mu.Unlock()
                },
            )(result)
        }(rec)

5. Wait for all workers:
   wg.Wait()

6. Return aggregated results:
   if len(errors) > 0:
       return results, fmt.Errorf("\n  %s", strings.Join(errors, "\n  "))
   return results, nil
```

**Key behaviors:**
- `sem.Acquire(context.Background(), 1)` blocks the reader goroutine when N workers are in-flight. This provides natural backpressure — FASTA reading pauses until a worker slot opens up.
- Workers run independently to completion — no cancellation on individual errors (best-effort).
- Results are collected in FASTA-read order (approximately), not completion order — the semaphore acquisition gates the read loop.
- If `Concurrency >= len(records)`, all records are dispatched immediately (equivalent to full parallelism).
- FASTA parse errors are recorded as per-record errors, not fatal — the loop continues to the next record.

### 7.9 Per-Record Pipeline (Unchanged)

The functional pipeline for a single record is completely unchanged. No new context parameter needed:

```
Input: T.Tuple3[ioehttp.Client, ScanRequest, seqio.Fasta]

F.Pipe4(
    input,
    buildSubmitRequester,      // POST /run     → SubmittedJob
    IOE.Chain(pollJob),        // GET /status   → CompletedJob (poll loop)
    IOE.Chain(downloadAndSave), // GET /result   → string (file path)
    toEither[error, string],   // execute       → Either[error, string]
)

Output: Either[error, string]   // Left = error, Right = file path
```

All functions are reused without modification. Each worker runs in its own goroutine with an independent `pollJob` timeout.

### 7.10 Error Aggregation

Each worker records its outcome (success path or error message). After all workers complete via `wg.Wait()`, the orchestrator returns the aggregated result:

- If any errors were collected: return the successful paths alongside a multi-line error string containing all per-record failures.
- If no errors: return all paths with nil error.

FASTA parse errors (malformed records) are treated the same as processing errors — recorded and skipped, not fatal.

```
// After wg.Wait():
if len(errors) > 0 {
    return results, fmt.Errorf(
        "%d record(s) failed:\n  %s",
        len(errors),
        strings.Join(errors, "\n  "),
    )
}
return results, nil
```

### 7.11 Result Reporting

Updated `reportScanResults` for concurrent mode:

```
--- Scan Summary ---
Records processed:  100
  Succeeded:        97
  Failed:           3
Output directory:   ./output/

Failures:
  seq tr|Q95Q25: timed out after 30m0s
  seq sp|P12345: 500 Internal Server Error
  seq tr|Q9XYZ1: API returned status FAILED
```


---

## 8. Testing Strategy

### 8.1 Unit Tests

All existing tests in `scan_test.go` should continue to pass unchanged:
- `TestExtractScanRequest` — add assertion for `Concurrency` field
- `TestValidateScanRequest` — unchanged
- `TestBuildSubmitRequester` — unchanged
- `TestSaveResult` — unchanged
- `TestPollJobFinished` — unchanged
- `TestPollJobTimeout` — unchanged
- `TestProcessOneFastaIntegration` — unchanged

### 8.2 New Unit Tests

| Test | What it covers |
|------|---------------|
| `TestProcessOneRecord` | Verifies `processOneRecord` composes submit→poll→download→save correctly |
| `TestProcessOneRecordError` | Verifies error propagation through `processOneRecord` |

### 8.3 Integration Tests (Concurrent)

These are the critical new tests:

```
TestConcurrentScanTwoRecords
  - Setup: mock server with /run, /status, /result endpoints
  - Submit two records concurrently
  - Verify both JSON files are created
  - Verify both paths are returned

TestConcurrentScanSomeRecordsFail
  - Setup: mock server where some records fail (e.g., odd-indexed records return 500)
  - Submit multiple records concurrently
  - Verify successful records have their JSON files created
  - Verify failed records appear in the error summary
  - Verify processing continues past individual failures (best-effort)

TestConcurrentScanAllFinish
  - Setup: mock server that responds quickly
  - Submit N records (N > 25 to test semaphore backpressure)
  - Verify all N files are created
  - Verify error list is empty

TestConcurrentScanAllFail
  - Setup: mock server that always returns 500
  - Submit multiple records
  - Verify all records appear in error summary
  - Verify no files were created

TestConcurrentScanTimeout
  - Setup: mock server that always returns RUNNING status
  - Use short per-job timeout
  - Verify timeout errors appear for each record
  - Verify other records are not affected (best-effort)

TestConcurrentScanSemaphoreLimiting
  - Setup: mock server with artificial delay
  - Track max concurrent in-flight requests
  - Verify max never exceeds concurrency limit

TestConcurrentScanParseErrors
  - Setup: FASTA file with some malformed records (stray sequence data)
  - Verify parse errors appear in error summary
  - Verify valid records are processed normally
```

### 8.4 Test Fixtures

Reuse the existing mock server pattern from `scan_test.go`:

```go
// Multi-endpoint mock server (reuse from TestProcessOneFastaIntegration)
func newMockInterProServer(t *testing.T) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/plain")
        switch {
        case strings.HasSuffix(r.URL.Path, "/run"):
            io.WriteString(w, "JOB-TEST-"+randomID())
        case strings.Contains(r.URL.Path, "/status/"):
            io.WriteString(w, "FINISHED")
        case strings.Contains(r.URL.Path, "/result/"):
            w.Header().Set("Content-Type", "application/json")
            io.WriteString(w, `{"results":[]}`)
        }
    }))
}
```


---

## 9. Implementation Roadmap

### Phase 1: Foundation

1. **Add `Concurrency` field to `ScanRequest`** in `scan_model.go`.
2. **Add `concurrent-scan` subcommand** in `cmd/interpro-cli/main.go` with all scan flags + `--concurrency` (default 25).
3. **Add `processOneRecord` function** to new `scan_concurrent.go` — composes the existing pipeline functions.
4. **Implement `ConcurrentScan` entry point** — validation, mkdir, dispatch to `streamFastaRecordsConcurrent`.
5. **Write `TestProcessOneRecord`** — verify the composed pipeline works.
6. **Run full test suite** — verify no regressions.

### Phase 2: Concurrent Orchestrator

7. **Implement `streamFastaRecordsConcurrent`** in `scan_concurrent.go` using semaphore + WaitGroup.
8. **Add dependency:** `golang.org/x/sync` to `go.mod` (provides `semaphore`).
9. **Implement `reportConcurrentResults`** — prints summary with success/failure counts.
10. **Write `TestConcurrentScanTwoRecords`** — basic concurrent test.
11. **Write `TestConcurrentScanAllFinish`** — test with N > 25 records to verify semaphore backpressure.
12. **Write `TestConcurrentScanSomeFail`** — verify best-effort: one failure doesn't block others.

### Phase 3: Polish

13. **Write additional edge-case tests** (empty FASTA, all records fail, parse errors in stream).
14. **Add logging** — `IOE.ChainFirstIOK(IO.Logf[...])` for each record's progress.
15. **Run lint + full test suite.**
16. **Benchmark:** time a 100-record FASTA with `scan` (sequential) vs. `concurrent-scan` (25 concurrency).

### Phase 4: Optional Enhancements

17. **Fail-fast mode** (`--fail-fast` flag) using `errgroup` for cancellation.
18. **Progress bar** — show "Processing 15/100 (3 failed)..." during execution.
19. **Retry logic** — retry failed submissions with exponential backoff.
20. **Rate limiting** — add `--rate-limit` flag for API rate limiting.


---

## 10. Dependencies

### New Go dependency

```
golang.org/x/sync v0.x.x
```

Provides:
- `golang.org/x/sync/semaphore` — weighted semaphore for concurrency bounding

(Note: `errgroup` is NOT needed — best-effort mode uses `sync.WaitGroup` from the standard library.)

### Existing dependencies (unchanged)

| Package | Version | Usage |
|---------|---------|-------|
| `github.com/IBM/fp-go/v2` | v2.3.11 | Functional pipeline (IOEither, Either, array, tuple, etc.) |
| `github.com/urfave/cli/v3` | v3.9.0 | CLI framework |
| `github.com/stretchr/testify` | v1.11.1 | Test assertions |


---

## 11. Risks & Mitigations

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| **API rate limiting** — EBI may throttle 25 concurrent submissions | Medium | Add `--rate-limit` flag in Phase 4; test with small batches first |
| **Memory pressure** — 25 concurrent poll goroutines with HTTP clients | Low | Each goroutine is lightweight (~4KB stack); 25 goroutines ≈ 100KB |
| **File write contention** — 25 concurrent file writes to same output dir | Low | Each record writes to a unique file name (`{SeqID}_{JobID}.json`); OS handles concurrent writes |
| **Test flakiness** — timing-dependent tests with semaphore | Low | Use short timeouts (50ms) and fast mock servers; avoid `time.Sleep` in tests |
| **Goroutine leak** — WaitGroup not properly waited | Low | `defer wg.Done()` pattern; `go vet` catches missing Done calls |
| **Backward compatibility** — existing users of sequential `scan` | Low | Sequential `scan` subcommand preserved unchanged; `concurrent-scan` is separate |


---

## 12. Decisions

| # | Question | Decision |
|---|----------|----------|
| **Q1** | Concurrency limit | CLI flag `--concurrency` with default 25 |
| **Q2** | Error handling | Best-effort: collect per-record errors, continue processing |
| **Q3** | New subcommand or modify `scan`? | New `concurrent-scan` subcommand |
| **Q4** | Result ordering | Non-deterministic (completion order) |
| **Q5** | Summary reporting | Print summary with counts + failure details |
| **Q6** | Polling timeout | Per-job timeout (unchanged) |
| **Q7** | Approach | **C:** Semaphore-bounded worker pool with WaitGroup |

---

*Document version: 2.0 — May 2026*
*Status: Design approved — ready for implementation* 

---

*Document version: 1.0 — May 2026*
