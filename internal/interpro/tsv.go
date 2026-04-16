package interpro

import (
	"os"

	E "github.com/IBM/fp-go/v2/either"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
)

func openOutputFile(path string) IOE.IOEither[error, *os.File] {
	return IOEF.Create(path)
}

func closeOutputFile(handle *os.File) IOE.IOEither[error, struct{}] {
	return IOEF.Close[*os.File](handle)
}

func writeHeader(handle *os.File) IOE.IOEither[error, []byte] {
	return IOE.TryCatchError(func() ([]byte, error) {
		data := []byte("accession\tname\tgene\n")
		_, err := handle.Write(data)
		return data, err
	})
}

func writeChunk(handle *os.File) func(string) IOE.IOEither[error, []byte] {
	return func(chunk string) IOE.IOEither[error, []byte] {
		return IOE.TryCatchError(func() ([]byte, error) {
			data := []byte(chunk)
			_, err := handle.Write(data)
			return data, err
		})
	}
}

func readFile(path string) E.Either[error, []byte] {
	return IOEF.ReadFile(path)()
}
