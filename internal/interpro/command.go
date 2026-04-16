// Package interpro provides functions to extract data from InterPro and write
// it to a TSV file.
package interpro

import (
	"context"
	"fmt"

	EF "github.com/IBM/fp-go/v2/effect"
	E "github.com/IBM/fp-go/v2/either"
	IOE "github.com/IBM/fp-go/v2/ioeither"
)

type ExtractConfig struct {
	StartURL   string
	OutputPath string
}

func liftToThunk[A any](ioe IOE.IOEither[error, A]) EF.Thunk[A] {
	return func(_ context.Context) func() E.Either[error, A] {
		return ioe
	}
}

func ExtractEffect() EF.Effect[ExtractConfig, string] {
	return EF.Chain(func(cfg ExtractConfig) EF.Effect[ExtractConfig, string] {
		program := IOE.Chain(func(records []ProteinRecord) IOE.IOEither[error, string] {
			return WriteTSV(cfg.OutputPath, records)
		})(FetchAllPages(cfg.StartURL))

		return EF.FromThunk[ExtractConfig](liftToThunk(program))
	})(EF.Ask[ExtractConfig]())
}

func ExtractAndWrite(startURL, outputPath string) error {
	program := EF.Provide[string](ExtractConfig{
		StartURL:   startURL,
		OutputPath: outputPath,
	})(ExtractEffect())

	result := program(context.Background())()

	return E.Fold(
		func(err error) error {
			return fmt.Errorf("extract failed: %w", err)
		},
		func(path string) error {
			fmt.Printf("wrote %s\n", path)
			return nil
		},
	)(result)
}
