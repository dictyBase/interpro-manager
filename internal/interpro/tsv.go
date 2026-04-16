package interpro

import (
	"fmt"
	"os"

	E "github.com/IBM/fp-go/v2/either"
	IOE "github.com/IBM/fp-go/v2/ioeither"
)

func WriteTSV(path string, records []ProteinRecord) IOE.IOEither[error, string] {
	content := FormatTSV(records)
	return IOE.TryCatchError(func() (string, error) {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing file %s: %w", path, err)
		}
		return path, nil
	})
}

func WriteTSVFold(path string, records []ProteinRecord) E.Either[error, string] {
	return WriteTSV(path, records)()
}
