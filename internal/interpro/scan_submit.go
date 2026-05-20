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
	T "github.com/IBM/fp-go/v2/tuple"

	"github.com/dictybase-docker/interpro-manager/internal/seqio"
)

func buildSubmitRequester(
	input T.Tuple2[SubmitArgs, seqio.Fasta],
) IOE.IOEither[error, SubmittedJob] {
	args := input.F1
	rec := input.F2
	client := args.F1
	scanConfig := args.F2

	formData := url.Values{
		"email":    {scanConfig.Email},
		"stype":    {scanConfig.SeqType},
		"goterms":  {"true"},
		"pathways": {"true"},
		"sequence": {fmt.Sprintf(">%s\n%s", string(rec.ID), string(rec.Sequence))},
	}.Encode()

	requester := F.Pipe6(
		B.Default,
		B.WithURL(scanConfig.BaseURL+"/run"),
		B.WithMethod("POST"),
		B.WithHeader("Content-Type")("application/x-www-form-urlencoded"),
		B.WithHeader("Accept")("text/plain"),
		B.WithBytes([]byte(formData)),
		ioehb.Requester,
	)

	return IOE.Map[error](func(jobID string) SubmittedJob {
		return SubmittedJob{
			JobID:  strings.TrimSpace(jobID),
			SeqID:  extractSeqID(rec),
			Client: client,
			Config: scanConfig,
		}
	})(ioehttp.ReadText(client)(requester))
}
