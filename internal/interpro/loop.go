package interpro

import (
	"fmt"
	"os"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	T "github.com/IBM/fp-go/v2/tuple"
)

func pageStepEither(step PageStep) E.Either[error, PageStep] {
	return E.FromPredicate(
		func(PageStep) bool { return stepError(step) == nil },
		func(PageStep) error { return stepError(step) },
	)(step)
}

func writeStep(handle *os.File) func(PageStep) E.Either[error, LoopStep] {
	return func(step PageStep) E.Either[error, LoopStep] {
		chunkResult := writeChunk(handle)(stepChunk(step))()
		return E.Map[error](func([]byte) LoopStep {
			return T.MakeTuple3[error, string, string](
				nil,
				stepNext(step),
				"",
			)
		})(chunkResult)
	}
}

func runLoop(state RuntimeState) IOE.IOEither[error, string] {
	return IOE.TryCatchError(func() (string, error) {
		currentURL := runtimeURL(state)
		handle := runtimeHandle(state)
		client := runtimeClient(state)
		outputPath := runtimeOutputPath(state)
		last := T.MakeTuple3[error, string, string](nil, currentURL, outputPath)

		for currentURL != "" {
			last = F.Pipe4(
				currentURL,
				fetchPageStep(client),
				pageStepEither,
				E.Chain(writeStep(handle)),
				E.Fold(
					func(err error) LoopStep {
						return T.MakeTuple3[error, string, string](err, "", outputPath)
					},
					func(next LoopStep) LoopStep {
						return T.MakeTuple3[error, string, string](
							loopError(next),
							loopNext(next),
							outputPath,
						)
					},
				),
			)

			currentURL = loopNext(last)
		}

		if loopError(last) != nil {
			return "", fmt.Errorf("extract failed: %w", loopError(last))
		}

		return loopOutput(last), nil
	})
}
