package interpro

import (
	"fmt"
	"net/url"
	"strings"

	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	ioehb "github.com/IBM/fp-go/v2/ioeither/http/builder"
	M "github.com/IBM/fp-go/v2/monoid"
	STR "github.com/IBM/fp-go/v2/string"
	"github.com/dictybase/interpro-manager/internal/seqio"
)

func fastaEntry(rec seqio.Fasta) string {
	return fmt.Sprintf(
		">%s\n%s",
		string(rec.ID),
		strings.ReplaceAll(string(rec.Sequence), "*", ""),
	)
}

func buildSubmitRequester(
	input SubmitInput,
) IOE.IOEither[error, SubmittedJob] {
	client := input.F1
	scanConfig := input.F2
	rec := input.F3

	formData := url.Values{
		"email":    {scanConfig.Email},
		"stype":    {scanConfig.SeqType},
		"goterms":  {"true"},
		"pathways": {"true"},
		"sequence": {fastaEntry(rec)},
	}.Encode()

	return F.Pipe2(
		F.Pipe6(
			B.Default,
			F.Pipe1(
				M.ConcatAll(STR.Monoid)([]string{
					scanConfig.BaseURL,
					"/",
					"run",
				}),
				B.WithURL,
			),
			B.WithMethod("POST"),
			B.WithHeader("Content-Type")("application/x-www-form-urlencoded"),
			B.WithHeader("Accept")("text/plain"),
			B.WithBytes([]byte(formData)),
			ioehb.Requester,
		),
		ioehttp.ReadText(client),
		IOE.Map[error](func(jobID string) SubmittedJob {
			return SubmittedJob{
				JobID:  strings.TrimSpace(jobID),
				SeqID:  extractSeqID(rec),
				Client: client,
				Config: scanConfig,
			}
		}),
	)
}
