package interpro

import (
	"net/http"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	B "github.com/IBM/fp-go/v2/http/builder"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	ioehb "github.com/IBM/fp-go/v2/ioeither/http/builder"
	O "github.com/IBM/fp-go/v2/option"
	T "github.com/IBM/fp-go/v2/tuple"
)

func toEither[ERR, A any](ioe IOE.IOEither[ERR, A]) E.Either[ERR, A] {
	return ioe()
}

func nextURL(next *string) string {
	return O.Fold(
		func() string { return "" },
		func(url string) string { return url },
	)(O.Map(func(p *string) string { return *p })(O.FromNillable(next)))
}

func fetchPageStep(client ioehttp.Client) func(string) PageStep {
	return func(url string) PageStep {
		return F.Pipe5(
			B.Default,
			B.WithURL(url),
			ioehb.Requester,
			ioehttp.ReadJSON[APIResponse](client),
			toEither,
			E.Fold(
				func(err error) PageStep {
					return T.MakeTuple4[error, string, []ProteinRecord, string](
						err,
						"",
						nil,
						"",
					)
				},
				func(resp APIResponse) PageStep {
					return F.Pipe2(
						resp,
						toPageData,
						func(data PageData) PageStep {
							return T.MakeTuple4[error, string, []ProteinRecord, string](
								nil,
								FormatTSVChunk(pageRows(data)),
								pageRows(data),
								pageNext(data),
							)
						},
					)
				},
			),
		)
	}
}

func MakeHTTPClient() ioehttp.Client {
	return ioehttp.MakeClient(http.DefaultClient)
}
