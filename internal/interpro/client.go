package interpro

import (
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
	return F.Pipe3(
		next,
		O.FromNillable[string],
		O.Map(func(p *string) string { return *p }),
		O.GetOrElse(F.Constant("")),
	)
}

func fetchPage(cfg FetchConfig) IOE.IOEither[error, PageStep] {
	return F.Pipe4(
		B.Default,
		B.WithURL(cfg.F2),
		ioehb.Requester,
		ioehttp.ReadJSON[APIResponse](cfg.F1),
		IOE.Map[error](func(resp APIResponse) PageStep {
			return T.MakeTuple2(
				FormatTSVChunk(resp.Results),
				nextURL(resp.Next),
			)
		}),
	)
}
