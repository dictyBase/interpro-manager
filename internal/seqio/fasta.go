// Package seqio is a generic namespace shared by all biological sequence input and output
// handlers.
// This package contain a very barebone and simple Fasta format sequence file parser.
// Currently, it parses and returns the Id(header) and sequence.
// This is mostly working concept, however could be easily extended in future.
// Example:
//
//	for r := range seqio.ParseFASTA("sequences.fa") {
//	    result.MonadFold(r,
//	        func(err error) any { /* handle malformed record / I/O */ },
//	        func(rec seqio.Fasta) any { /* use rec */ },
//	    )
//	}
package seqio

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOR "github.com/IBM/fp-go/v2/ioresult"
	Iter "github.com/IBM/fp-go/v2/iterator/iter"
	IR "github.com/IBM/fp-go/v2/iterator/iterresult"
	O "github.com/IBM/fp-go/v2/option"
	P "github.com/IBM/fp-go/v2/pair"
	Pred "github.com/IBM/fp-go/v2/predicate"
	R "github.com/IBM/fp-go/v2/result"
	Str "github.com/IBM/fp-go/v2/string"
)

// Domain types

// Fasta is a struct representing a fasta record, containing the sequence ID and
// the sequence itself.
type Fasta struct {
	ID       []byte
	Sequence []byte
}

// lineEvent is a sum type for a classified input line. Two variants:
//   - header(payload): line starts with ">" (payload = text after ">")
//   - seqData(payload): any other line
//
// Encoded as Either[string, string] where Left = header, Right = seqData.
type lineEvent = E.Either[string, string]

// inProgress is the parser state. Option models "are we currently
// accumulating a record?" — None before the first header, Some after.
type inProgress struct {
	header []byte
	seq    []byte
}

type parseState = O.Option[inProgress]

// Layer 1: pure line classifier

const headerPrefix = ">"

var (
	// isHeaderLine is a Predicate[string]: true when a line starts with ">".
	isHeaderLine = Str.HasPrefix(headerPrefix)

	// trimHeaderPrefix strips the leading ">" from a raw header line.
	trimHeaderPrefix = F.Bind2of2(strings.TrimPrefix)(headerPrefix)

	// classify is point-free: no explicit func body, no if/else, no named arg.
	// Pred.Not(isHeaderLine) is true for sequence lines  -> Right(line).
	// When false (header lines)                          -> Left(trimPrefix(line)).
	classify = E.FromPredicate(Pred.Not(isHeaderLine), trimHeaderPrefix)
)

// Layer 2: pure state transition

// toRightFasta lifts an inProgress into a successful Result[Fasta].
func toRightFasta(p inProgress) R.Result[Fasta] {
	return R.Of(Fasta{ID: p.header, Sequence: p.seq})
}

// step is a pure (state, event) -> (state, Option[Result[Fasta]]) function.
// The Option in the second slot encodes "did this step emit a record?"
// (None = continue accumulating, Some = flush this record now).
func step(s parseState, ev lineEvent) P.Pair[parseState, O.Option[R.Result[Fasta]]] {
	return E.MonadFold(ev,
		// Left branch: header line — start new record, flush any in progress.
		func(hdr string) P.Pair[parseState, O.Option[R.Result[Fasta]]] {
			fresh := O.Some(inProgress{header: []byte(hdr)})
			return P.MakePair(fresh, O.Map(toRightFasta)(s))
		},
		// Right branch: sequence line — append if in record, else error.
		func(seqLine string) P.Pair[parseState, O.Option[R.Result[Fasta]]] {
			return O.MonadFold(s,
				// None: stray sequence before any header
				func() P.Pair[parseState, O.Option[R.Result[Fasta]]] {
					return P.MakePair(s, O.Some(R.Left[Fasta](
						fmt.Errorf("sequence data before header: %q", seqLine),
					)))
				},
				// Some: append, no emission
				func(curr inProgress) P.Pair[parseState, O.Option[R.Result[Fasta]]] {
					next := inProgress{
						header: curr.header,
						seq:    append(curr.seq, seqLine...),
					}
					return P.MakePair(O.Some(next), O.None[R.Result[Fasta]]())
				},
			)
		},
	)
}

// Layer 3: line stream — pure adapter

// linesOf is a thin adapter from *os.File to Iter.Seq[string].
// Has zero FASTA knowledge; could be reused for any line-oriented format.
func linesOf(file *os.File) Iter.Seq[string] {
	return func(yield func(string) bool) {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if !yield(scanner.Text()) {
				return
			}
		}
	}
}

// Layer 4: iterator shell — minimal imperative glue

// assembleRecords drives step over a line stream, projecting events
// into the SeqResult yield protocol.
func assembleRecords(lines Iter.Seq[string]) IR.SeqResult[Fasta] {
	return func(yield func(R.Result[Fasta]) bool) {
		state := O.None[inProgress]()

		emit := func(em O.Option[R.Result[Fasta]]) bool {
			return O.MonadFold(em,
				func() bool { return true }, // None: nothing to emit
				func(r R.Result[Fasta]) bool { return yield(r) },
			)
		}

		for line := range lines {
			next := step(state, classify(line))
			state = P.Head(next)
			if !emit(P.Tail(next)) {
				return
			}
		}
		// Flush trailing in-progress record (if any).
		O.MonadFold(state,
			func() any { return nil },
			func(p inProgress) any { yield(toRightFasta(p)); return nil },
		)
	}
}

// Layer 5: public entry — Bracket binds resource to stream

// ParseFASTA opens a FASTA file and returns a SeqResult[Fasta] — a lazy
// stream where each element is Right(Fasta) or Left(error). Per-record
// errors emit Left and continue; the file is deterministically closed
// when iteration finishes (EOF, error, or break).
func ParseFASTA(path string) IR.SeqResult[Fasta] {
	acquire := IR.FromIOResult(IOR.TryCatchError(
		func() (*os.File, error) { return os.Open(path) },
	))
	use := F.Flow2(linesOf, assembleRecords)
	release := func(f *os.File, _ IR.Result[Fasta]) IR.SeqResult[F.Void] {
		return IR.FromIO(func() F.Void {
			_ = f.Close()
			return F.VOID
		})
	}
	return IR.Bracket(acquire, use, release)
}
