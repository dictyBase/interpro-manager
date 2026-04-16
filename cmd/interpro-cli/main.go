package main

import (
	"context"
	"fmt"
	"os"

	"github.com/dictybase-docker/interpro-manager/internal/interpro"
	"github.com/urfave/cli/v3"
)

const (
	defaultTaxonID = "44689"
	defaultOutput  = "interpro_proteins.tsv"
	baseURL        = "https://www.ebi.ac.uk/interpro/api/protein/UniProt/taxonomy/uniprot/"
)

func main() {
	cmd := &cli.Command{
		Name:  "interpro-manager",
		Usage: "CLI for interacting with the InterPro protein database",
		Commands: []*cli.Command{
			{
				Name:  "extract",
				Usage: "Extract protein metadata for a taxonomy ID and save to TSV",
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
						Value:   20,
						Usage:   "API page size",
					},
				},
				Action: extractAction,
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func extractAction(ctx context.Context, cmd *cli.Command) error {
	taxonID := cmd.String("taxon-id")
	output := cmd.String("output")
	pageSize := cmd.Int("page-size")

	startURL := fmt.Sprintf("%s%s/?page_size=%d", baseURL, taxonID, pageSize)

	return interpro.ExtractAndWrite(startURL, output)
}
