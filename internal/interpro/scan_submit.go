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

	"github.com/dictybase-docker/interpro-manager/internal/seqio"
)

// buildSubmitRequester: pure function — builds the form-encoded POST requester.
// Uses url.Values.Encode() instead of manual url.QueryEscape concatenation.
func buildSubmitRequester(scanConfig ScanRequest, rec seqio.Fasta) ioehttp.Requester {
	formData := url.Values{
		"email":    {scanConfig.Email},
		"stype":    {scanConfig.SeqType},
		"goterms":  {"true"},
		"pathways": {"true"},
		"sequence": {fmt.Sprintf(">%s\n%s", string(rec.ID), string(rec.Sequence))},
	}.Encode()

	return F.Pipe6(
		B.Default,
		B.WithURL(scanConfig.BaseURL+"/run"),
		B.WithMethod("POST"),
		B.WithHeader("Content-Type")("application/x-www-form-urlencoded"),
		B.WithHeader("Accept")("text/plain"),
		B.WithBytes([]byte(formData)),
		ioehb.Requester,
	)
}

// submitOneRecord: SubmitArgs → Fasta → IOEither[error, SubmittedJob]
func submitOneRecord(args SubmitArgs) func(seqio.Fasta) IOE.IOEither[error, SubmittedJob] {
	client, config := args.F1, args.F2
	return func(rec seqio.Fasta) IOE.IOEither[error, SubmittedJob] {
		return F.Pipe2(
			buildSubmitRequester(config, rec),
			ioehttp.ReadText(client),
			IOE.Map[error](func(jobID string) SubmittedJob {
				return SubmittedJob{
					JobID:  strings.TrimSpace(jobID),
					SeqID:  extractSeqID(rec),
					Client: client,
					Config: config,
				}
			}),
		)
	}
}

// extractSeqID returns the first whitespace-delimited token of the FASTA
// header, which is the UniProt/TrEMBL accession number.
func extractSeqID(rec seqio.Fasta) string {
	return strings.Fields(string(rec.ID))[0]
}
