package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dictybase/interpro-manager/internal/interpro"
	"github.com/urfave/cli/v3"
)

const (
	defaultTaxonID      = "44689"
	defaultOutput       = "interpro_proteins.tsv"
	defaultPageSize     = 20
	defaultPollInterval = 15 * time.Second
	defaultTimeout      = 30 * time.Minute
)

func main() {
	cmd := &cli.Command{
		Name:  "interpro-manager",
		Usage: "CLI for interacting with the InterPro protein database",
		Commands: []*cli.Command{
			{
				Name:  "download",
				Usage: "Fetch InterPro protein records for a taxonomy ID and save to TSV",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "taxon-id",
						Aliases: []string{"t"},
						Value:   defaultTaxonID,
						Usage:   "NCBI Taxonomy ID",
					},
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Value:   defaultOutput,
						Usage:   "Output TSV file path",
					},
					&cli.IntFlag{
						Name:    "page-size",
						Aliases: []string{"p"},
						Value:   defaultPageSize,
						Usage:   "API page size",
					},
				},
				Action: interpro.DownloadAndWrite,
			},
			{
				Name:  "scan",
				Usage: "Submit protein sequences to InterProScan and save JSON results",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "fasta",
						Aliases: []string{"f"},
						Usage:   "Path to FASTA file (supports multi-FASTA)",
					},
					&cli.StringFlag{
						Name:    "email",
						Aliases: []string{"e"},
						Sources: cli.EnvVars("EBI_EMAIL"),
						Usage:   "Email for EMBL-EBI Job Dispatcher (required)",
					},
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Value:   ".",
						Usage:   "Output directory for JSON results",
					},
					&cli.StringFlag{
						Name:    "seq-type",
						Aliases: []string{"s"},
						Value:   "p",
						Usage:   "Sequence type: p (protein) or n (nucleotide)",
					},
					&cli.DurationFlag{
						Name:  "poll-interval",
						Value: defaultPollInterval,
						Usage: "How often to check job status",
					},
					&cli.DurationFlag{
						Name:  "timeout",
						Value: defaultTimeout,
						Usage: "Maximum time to wait for a single job",
					},
				},
				Action: interpro.Scan,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
