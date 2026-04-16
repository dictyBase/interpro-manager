package interpro

import (
	"os"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
)

const filePerm os.FileMode = 0o600

func WriteTSV(path string, records []ProteinRecord) IOE.IOEither[error, string] {
	return F.Pipe2(
		[]byte(FormatTSV(records)),
		IOEF.WriteFile(path, filePerm),
		IOE.Map[error](func([]byte) string { return path }),
	)
}

func WriteTSVFold(path string, records []ProteinRecord) E.Either[error, string] {
	return WriteTSV(path, records)()
}
