package interpro

import (
	"fmt"
	"net/url"
	"os"

	"github.com/IBM/fp-go/v2/file"

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
	return F.Pipe6(
		B.Default,
		F.Pipe2(job, resultURL, B.WithURL),
		B.WithHeader("Accept")("application/json"),
		ioehb.Requester,
		ioehttp.ReadText(job.Client),
		IOE.Map[error](func(rawJSON string) T.Tuple3[string, CompletedJob, string] {
			output := F.Pipe1(
				job.Config.OutputDir,
				file.Join(fmt.Sprintf(
					"%s_%s.json",
					job.SeqID,
					job.JobID,
				)),
			)
			return T.MakeTuple3(rawJSON, job, output)
		}),
		IOE.Chain(saveResult),
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

func saveResult(input T.Tuple3[string, CompletedJob, string]) IOE.IOEither[error, string] {
	return F.Pipe3(
		input.F3,
		IOEF.Create,
		IOEF.WriteAll[*os.File]([]byte(input.F1)),
		IOE.Map[error](func(_ []byte) string {
			return input.F3
		}),
	)
}
