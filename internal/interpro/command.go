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

func writeRuntimeHeader(state RuntimeState) IOE.IOEither[error, []byte] {
	return F.Pipe2(
		state,
		runtimeHandle,
		writeHeader,
	)
}

func ExtractAndWrite(_ context.Context, cmd *cli.Command) error {
	return F.Pipe3(
		cmd,
		initialConfig,
		runProgram,
		E.Fold(wrapRunError, reportSuccess),
	)
}

func initialConfig(cmd *cli.Command) T.Tuple3[ioehttp.Client, string, string] {
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

func runProgram(cfg ExtractConfig) E.Either[error, string] {
	return IOE.WithResource[string](
		F.Pipe3(
			cfg.F3,
			IOEF.Create,
			IOE.Map[error](newRuntimeState(cfg)),
			IOE.ChainFirst(writeRuntimeHeader),
		),
		func(state RuntimeState) IOE.IOEither[error, struct{}] {
			return closeOutputFile(runtimeHandle(state))
		},
	)(runLoop)()
}
