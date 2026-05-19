package interpro

import (
	"fmt"
	"os"
	"path/filepath"

	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	ioehb "github.com/IBM/fp-go/v2/ioeither/http/builder"
)

// downloadAndSave: CompletedJob → IOEither[error, string]
func downloadAndSave(job CompletedJob) IOE.IOEither[error, string] {
	return F.Pipe1(
		downloadJSONResult(job),
		IOE.Chain(saveResult(job)),
	)
}

// downloadJSONResult fetches the JSON result body for a completed job.
func downloadJSONResult(job CompletedJob) IOE.IOEither[error, string] {
	return F.Pipe4(
		B.Default,
		B.WithURL(fmt.Sprintf("%s/result/%s/json", job.Config.BaseURL, job.JobID)),
		B.WithHeader("Accept")("application/json"),
		ioehb.Requester,
		ioehttp.ReadText(job.Client),
	)
}

// saveResult: curried — saveResult(job)(rawJSON) writes JSON to disk.
func saveResult(job CompletedJob) func(rawJSON string) IOE.IOEither[error, string] {
	return func(rawJSON string) IOE.IOEither[error, string] {
		outputPath := filepath.Join(
			job.Config.OutputDir,
			fmt.Sprintf("%s_%s.json", job.SeqID, job.JobID),
		)
		return IOE.WithResource[string](
			IOEF.Create(outputPath),
			func(f *os.File) IOE.IOEither[error, F.Void] {
				return IOE.TryCatchError(func() (F.Void, error) {
					return F.VOID, f.Close()
				})
			},
		)(func(handle *os.File) IOE.IOEither[error, string] {
			return IOE.TryCatchError(func() (string, error) {
				_, err := handle.Write([]byte(rawJSON))
				return outputPath, err
			})
		})
	}
}
