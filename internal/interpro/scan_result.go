package interpro

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	IOEF "github.com/IBM/fp-go/v2/ioeither/file"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	ioehb "github.com/IBM/fp-go/v2/ioeither/http/builder"
	M "github.com/IBM/fp-go/v2/monoid"
	STR "github.com/IBM/fp-go/v2/string"
	T "github.com/IBM/fp-go/v2/tuple"
)

// downloadAndSave: CompletedJob → IOEither[error, string]
func downloadAndSave(job CompletedJob) IOE.IOEither[error, string] {
	return F.Pipe2(
		downloadJSONResult(job),
		IOE.Map[error](func(rawJSON string) T.Tuple2[string, CompletedJob] {
			return T.MakeTuple2(rawJSON, job)
		}),
		IOE.Chain(saveResult),
	)
}

// downloadJSONResult fetches the JSON result body for a completed job.
func downloadJSONResult(job CompletedJob) IOE.IOEither[error, string] {
	return F.Pipe4(
		B.Default,
		F.Pipe2(job, resultURL, B.WithURL),
		B.WithHeader("Accept")("application/json"),
		ioehb.Requester,
		ioehttp.ReadText(job.Client),
	)
}

func resultURL(job CompletedJob) string {
	return M.ConcatAll(STR.Monoid)([]string{
		job.Config.BaseURL,
		"/result/",
		url.PathEscape(job.JobID),
		"/json",
	})
}

// saveResult: T.Tuple2[string, CompletedJob] → IOEither[error, string]
func saveResult(input T.Tuple2[string, CompletedJob]) IOE.IOEither[error, string] {
	rawJSON, job := input.F1, input.F2
	outputPath := filepath.Join(
		job.Config.OutputDir,
		fmt.Sprintf("%s_%s.json", job.SeqID, job.JobID),
	)
	return F.Pipe1(
		F.Pipe1(
			IOEF.Create(outputPath),
			IOEF.WriteAll[*os.File]([]byte(rawJSON)),
		),
		IOE.Map[error](func(_ []byte) string { return outputPath }),
	)
}
