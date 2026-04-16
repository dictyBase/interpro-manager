package interpro

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	O "github.com/IBM/fp-go/v2/option"
)

//nolint:bodyclose
func makeRequest(url string) E.Either[error, *http.Response] {
	return E.Chain(
		func(req *http.Request) E.Either[error, *http.Response] {
			return E.TryCatchError(http.DefaultClient.Do(req))
		},
	)(E.TryCatchError(http.NewRequest(http.MethodGet, url, nil)))
}

//nolint:bodyclose
func assertOK(resp *http.Response) E.Either[error, *http.Response] {
	return E.FromPredicate(
		func(r *http.Response) bool { return r.StatusCode == http.StatusOK },
		func(r *http.Response) error {
			return fmt.Errorf("unexpected status code: %d", r.StatusCode)
		},
	)(resp)
}

func readBody(body io.ReadCloser) IOE.IOEither[error, []byte] {
	return IOE.WithResource[[]byte](
		IOE.Of[error](body),
		func(c io.ReadCloser) IOE.IOEither[error, struct{}] {
			return IOE.TryCatchError(func() (struct{}, error) {
				return struct{}{}, c.Close()
			})
		},
	)(func(c io.ReadCloser) IOE.IOEither[error, []byte] {
		return IOE.TryCatchError(func() ([]byte, error) {
			return io.ReadAll(c)
		})
	})
}

//nolint:bodyclose
func fetchURL(url string) IOE.IOEither[error, []byte] {
	return F.Pipe3(
		makeRequest(url),
		E.Chain(assertOK),
		IOE.FromEither[error, *http.Response],
		IOE.Chain(func(resp *http.Response) IOE.IOEither[error, []byte] {
			return readBody(resp.Body)
		}),
	)
}

func decodeResponse(data []byte) E.Either[error, APIResponse] {
	var resp APIResponse
	return E.TryCatchError(resp, json.Unmarshal(data, &resp))
}

func fetchPage(url string) IOE.IOEither[error, APIResponse] {
	return IOE.Chain(func(data []byte) IOE.IOEither[error, APIResponse] {
		return IOE.FromEither[error](decodeResponse(data))
	})(fetchURL(url))
}

func nextOption(next *string) O.Option[string] {
	return F.Pipe1(
		O.FromNillable(next),
		O.Map(func(p *string) string { return *p }),
	)
}

func FetchAllPages(startURL string) IOE.IOEither[error, []ProteinRecord] {
	var loop func(string, []ProteinRecord) IOE.IOEither[error, []ProteinRecord]

	loop = func(url string, acc []ProteinRecord) IOE.IOEither[error, []ProteinRecord] {
		return IOE.Chain(func(resp APIResponse) IOE.IOEither[error, []ProteinRecord] {
			records := ExtractRecords(resp.Results)
			all := append(acc, records...)

			return O.Fold(
				func() IOE.IOEither[error, []ProteinRecord] {
					return IOE.Of[error](all)
				},
				func(next string) IOE.IOEither[error, []ProteinRecord] {
					return loop(next, all)
				},
			)(nextOption(resp.Next))
		})(fetchPage(url))
	}

	return loop(startURL, []ProteinRecord{})
}
