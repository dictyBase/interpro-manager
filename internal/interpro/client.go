package interpro

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	E "github.com/IBM/fp-go/v2/either"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	O "github.com/IBM/fp-go/v2/option"
)

func fetchURL(url string) IOE.IOEither[error, []byte] {
	return IOE.TryCatchError(func() ([]byte, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("http new request failed: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http get failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading response body: %w", err)
		}
		return body, nil
	})
}

func decodeResponse(data []byte) E.Either[error, APIResponse] {
	var resp APIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return E.Left[APIResponse, error](fmt.Errorf("json decode: %w", err))
	}
	return E.Right[error, APIResponse](resp)
}

func fetchPage(url string) IOE.IOEither[error, APIResponse] {
	return IOE.Chain(func(data []byte) IOE.IOEither[error, APIResponse] {
		return IOE.FromEither[error](decodeResponse(data))
	})(fetchURL(url))
}

func nextOption(next *string) O.Option[string] {
	if next == nil {
		return O.None[string]()
	}
	return O.Some(*next)
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
