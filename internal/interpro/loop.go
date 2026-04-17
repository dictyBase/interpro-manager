package interpro

import (
	"fmt"
	"os"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	S "github.com/IBM/fp-go/v2/string"
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
			return T.MakeTuple3[error](
				nil,
				stepNext(step),
				"",
			)
		})(chunkResult)
	}
}

func runLoop(state RuntimeState) IOE.IOEither[error, string] {
	return IOE.TryCatchError(func() (string, error) {
		currentURL := state.F2
		handle := state.F4
		client := state.F1
		outputPath := state.F3
		last := T.MakeTuple3[error](nil, currentURL, outputPath)

		for S.IsNonEmpty(currentURL) {
			last = F.Pipe4(
				currentURL,
				fetchPageStep(client),
				pageStepEither,
				E.Chain(writeStep(handle)),
				E.Fold(
					func(err error) LoopStep {
						return T.MakeTuple3(err, "", outputPath)
					},
					func(next LoopStep) LoopStep {
						return T.MakeTuple3(
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
