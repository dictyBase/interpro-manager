package interpro

import (
	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	S "github.com/IBM/fp-go/v2/string"
	T "github.com/IBM/fp-go/v2/tuple"
)

func writePage(cfg WriteConfig) IOE.IOEither[error, string] {
	return IOE.TryCatchError(func() (string, error) {
		_, err := cfg.F1.WriteString(cfg.F2)
		return cfg.F3, err
	})
}

func runLoop(state RuntimeState) IOE.IOEither[error, string] {
	return IOE.TryCatchError(func() (string, error) {
		currentURL := state.F2
		for S.IsNonEmpty(currentURL) {
			var loopErr error
			currentURL = F.Pipe5(
				T.MakeTuple2(state.F1, currentURL),
				fetchPage,
				IOE.Map[error](func(step PageStep) WriteConfig {
					return T.MakeTuple3(state.F4, step.F1, step.F2)
				}),
				IOE.Chain(writePage),
				toEither[error, string],
				E.Fold(
					func(err error) string { loopErr = err; return "" },
					func(next string) string { return next },
				),
			)

			if loopErr != nil {
				return "", loopErr
			}
		}

		return state.F3, nil
	})
}
