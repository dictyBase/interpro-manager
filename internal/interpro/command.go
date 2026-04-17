// Package interpro provides the implementation of the `interpro extract`
// command, which extracts InterProScan results from a given input file and
// writes them to an output file in a specified format.
package interpro

import (
	"context"
	"fmt"
	"net/http"
	"os"

	IOEF "github.com/IBM/fp-go/v2/ioeither/file"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	T "github.com/IBM/fp-go/v2/tuple"
	"github.com/urfave/cli/v3"
)

const baseURL = "https://www.ebi.ac.uk/interpro/api/protein/UniProt/taxonomy/uniprot/"

func wrapRunError(err error) error {
	return fmt.Errorf("extract failed: %w", err)
}

func reportSuccess(path string) error {
	fmt.Printf("wrote %s\n", path)
	return nil
}

func writeRuntimeHeader(handle *os.File) IOE.IOEither[error, *os.File] {
	return IOE.TryCatchError(func() (*os.File, error) {
		_, err := handle.Write([]byte("accession\tname\tgene\n"))
		return handle, err
	})
}

func initialConfig(cmd *cli.Command) ExtractConfig {
	return T.MakeTuple3(
		ioehttp.MakeClient(http.DefaultClient),
		fmt.Sprintf(
			"%s%s/?page_size=%d",
			baseURL, cmd.String("taxon-id"),
			cmd.Int("page-size"),
		),
		cmd.String("output"),
	)
}

func onCreateFile(cfg ExtractConfig) IOE.IOEither[error, RuntimeState] {
	return F.Pipe3(
		cfg.F3,
		IOEF.Create,
		IOE.ChainFirst(writeRuntimeHeader),
		IOE.Map[error](func(handle *os.File) RuntimeState {
			return F.Pipe1(
				cfg,
				T.Push3[ioehttp.Client, string, string](handle),
			)
		}),
	)
}

func ExtractAndWrite(_ context.Context, cmd *cli.Command) error {
	return F.Pipe5(
		cmd,
		initialConfig,
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
		E.Fold(wrapRunError, reportSuccess),
	)
}
