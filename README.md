# interpro-manager

[![Go Reference](https://pkg.go.dev/badge/github.com/dictybase/interpro-manager.svg)](https://pkg.go.dev/github.com/dictybase/interpro-manager)
[![Go Report Card](https://goreportcard.com/badge/github.com/dictybase/interpro-manager)](https://goreportcard.com/report/github.com/dictybase/interpro-manager)
[![CI/CD](https://github.com/dictybase/interpro-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/dictybase/interpro-manager/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-BSD%202--Clause-blue.svg)](LICENSE)
[![Funding](https://badgen.net/badge/NIGMS/Rex%20L%20Chisholm,K%20M%20Scaglione,Siddhartha%20Basu,dictyBase,DCR/yellow?list=|)](https://reporter.nih.gov/search/Z4n-a8mfeE6moXhk-OR3hw/project-details/11116605)

CLI tool for interacting with EMBL-EBI's InterPro protein database — download protein records by taxonomy and submit sequences to InterProScan6 for analysis.

## Contents

- [Prerequisites](#prerequisites)
- [Install](#install)
- [Commands](#commands)
  - [download](#download)
  - [scan](#scan)
- [Project Structure](#project-structure)
  - [Packages](#packages)
- [Development](#development)

## Prerequisites

- [Go](https://go.dev/) 1.25+
- A valid email address (required for scan — any email works)

## Install

```bash
go install github.com/dictybase/interpro-manager/cmd/interpro-cli@latest
```

Or build from source:

```bash
git clone https://github.com/dictybase/interpro-manager.git
cd interpro-manager
go build -o interpro-manager ./cmd/interpro-cli
```

## Commands

### download

Fetch InterPro protein records for a taxonomy ID, filter results that contain a gene symbol, and save as TSV.

```
interpro-manager download [--taxon-id ID] [--output FILE] [--page-size N]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--taxon-id` | `44689` | NCBI taxonomy ID (default: *Dictyostelium discoideum*) |
| `--output` | `interpro_proteins.tsv` | Output TSV file path |
| `--page-size` | `20` | Records per API page |

**Example:**

```bash
# Download D. discoideum proteins (default)
interpro-manager download

# Download proteins for a different organism with custom output
interpro-manager download --taxon-id 9606 --output human_proteins.tsv --page-size 50
```

The TSV output contains the following columns:

| Column | Source |
|--------|--------|
| Accession | Protein accession |
| Source Database | Source organism database |
| Gene | Gene symbol |
| Name | Protein name |
| Length | Sequence length |
| Source Organism | Organism name |

### scan

Submit protein sequences from FASTA files to the InterProScan6 job dispatcher, poll for completion, and download JSON results.

```
interpro-manager scan --fasta FILE --email ADDRESS [--output DIR] [--seq-type TYPE] [--poll-interval DURATION] [--timeout DURATION]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--fasta` | *(required)* | Path to FASTA file with protein sequences |
| `--email` | env: `EBI_EMAIL` | Email address for job submission (any valid email) |
| `--output` | `interproscan_results` | Output directory for JSON result files |
| `--seq-type` | `p` | Sequence type (`p` for protein, `n` for nucleotide) |
| `--poll-interval` | `10s` | Interval between job status checks |
| `--timeout` | `120s` | Maximum time to wait for job completion |

**Example:**

```bash
# Set email and scan a FASTA file
export EBI_EMAIL=yourname@example.com
interpro-manager scan --fasta proteins.fa

# Custom output directory with shorter polling
interpro-manager scan \
  --fasta proteins.fa \
  --email yourname@example.com \
  --output results/ \
  --poll-interval 5s \
  --timeout 300s
```

Results are saved as `{sequence-id}_{job-id}.json` in the output directory, one file per FASTA record.

## Project Structure

```
.
├── cmd/
│   └── interpro-cli/
│       └── main.go              # CLI entry point, command registration
├── internal/
│   ├── interpro/
│   │   ├── command.go           # download subcommand
│   │   ├── client.go            # HTTP client, JSON deserialization
│   │   ├── extract.go           # TSV formatting, gene filter
│   │   ├── loop.go              # Pagination loop for download
│   │   ├── model.go             # API response types
│   │   ├── scan.go              # scan subcommand orchestrator
│   │   ├── scan_loop.go         # FASTA record streaming
│   │   ├── scan_model.go        # Scan request/job types
│   │   ├── scan_poll.go         # Job status polling
│   │   ├── scan_result.go       # Result download
│   │   ├── scan_submit.go       # Job submission
│   │   └── tsv.go               # File I/O utilities
│   └── seqio/
│       ├── fasta.go             # FASTA parser
│       └── fasta_test.go        # FASTA parser tests
└── docs/
    └── ...                      # Design docs and reference material
```

### Packages

| Package | Responsibility |
|---------|---------------|
| `internal/interpro` | Core business logic — API clients, TSV generation, scan orchestration, job polling |
| `internal/seqio` | Pure functional FASTA parser using state-machine based iterators |

Both packages are built with [fp-go](https://github.com/IBM/fp-go) functional programming combinators and use [urfave/cli](https://github.com/urfave/cli) for the CLI framework.

## Development

```bash
# Run tests
gotestsum --format pkgname-and-test-fails --format-hide-empty-pkg -- ./...

# Run tests with verbose output
gotestsum --format testdox --format-hide-empty-pkg -- ./...

# Lint
golangci-lint run ./...

# Format
golangci-lint fmt

# Build
go build -o interpro-manager ./cmd/interpro-cli
```

## Sources

- [InterPro API documentation](https://www.ebi.ac.uk/interpro/api/static_files/swagger/)
- [InterProScan6 API](https://www.ebi.ac.uk/interpro/about/interproscan/)