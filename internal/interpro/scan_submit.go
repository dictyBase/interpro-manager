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
)

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
		"sequence": {fmt.Sprintf(">%s\n%s", string(rec.ID), string(rec.Sequence))},
	}.Encode()

	return F.Pipe2(
		F.Pipe6(
			B.Default,
			B.WithURL(scanConfig.BaseURL+"/run"),
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
