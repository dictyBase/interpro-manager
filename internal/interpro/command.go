package interpro

import (
	"fmt"
	"os"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	T "github.com/IBM/fp-go/v2/tuple"
)

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
	return F.Pipe1(
		runProgram(cfg),
		E.Fold(wrapRunError, reportSuccess),
	)
}

func runProgram(cfg ExtractConfig) E.Either[error, string] {
	return IOE.WithResource[string](
		F.Pipe2(
			openOutputFile(configOutputPath(cfg)),
			IOE.Map[error](newRuntimeState(cfg)),
			IOE.ChainFirst(func(state RuntimeState) IOE.IOEither[error, []byte] {
				return writeHeader(runtimeHandle(state))
			}),
		),
		func(state RuntimeState) IOE.IOEither[error, struct{}] {
			return closeOutputFile(runtimeHandle(state))
		},
	)(runLoop)()
}
