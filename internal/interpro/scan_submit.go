package interpro

import (
	"fmt"
	"net/url"
	"strings"

	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
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
