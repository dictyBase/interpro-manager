package seqio

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	F "github.com/IBM/fp-go/v2/function"
	Iter "github.com/IBM/fp-go/v2/iterator/iter"
	IR "github.com/IBM/fp-go/v2/iterator/iterresult"
	R "github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	headerRgxp = regexp.MustCompile(`^\w{1,4}`)
	seqRgxp    = regexp.MustCompile(`^[A-Z]+$`)
)

func collectRecords(t *testing.T, path string) []Fasta {
	t.Helper()
	records := slices.Collect(F.Pipe1(
		ParseFASTA(path),
		IR.GetOrElse(func(err error) Iter.Seq[Fasta] {
			require.NoError(t, err)
			return Iter.Empty[Fasta]()
		}),
	))
	return records
}

func TestSingleFasta(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err, "Could not get current directory")

	mfasta := filepath.Join(dir, "testdata", "single.fa")
	records := collectRecords(t, mfasta)

	require.Len(t, records, 1, "Expected exactly 1 record")
	assert.True(t, bytes.HasPrefix(records[0].ID, []byte("tr|Q95Q25")), "Expected to match header")
	assert.True(t, seqRgxp.Match(records[0].Sequence), "Expected to match sequence")
}

func TestMultiFasta(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err, "Could not get current directory")

	mfasta := filepath.Join(dir, "testdata", "multi.fa")
	records := collectRecords(t, mfasta)

	require.Len(t, records, 4, "Expected exactly 4 records")
	for _, record := range records {
		assert.True(t, headerRgxp.Match(record.ID), "Expected to match header")
		assert.True(t, seqRgxp.Match(record.Sequence), "Expected to match sequence")
	}
}

func TestParseFASTAYieldsLeftOnStraySequence(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err, "Could not get current directory")

	path := filepath.Join(dir, "testdata", "stray.fa")

	var lefts []error
	var rights []Fasta
	for r := range ParseFASTA(path) {
		R.MonadFold(r,
			func(err error) any { lefts = append(lefts, err); return nil },
			func(rec Fasta) any { rights = append(rights, rec); return nil },
		)
	}
	require.Len(t, lefts, 1, "Expected 1 Left (stray sequence error)")
	assert.Contains(t, lefts[0].Error(), "sequence data before header")
	require.Len(t, rights, 1, "Expected 1 Right (valid record after stray)")
	assert.True(t, headerRgxp.Match(rights[0].ID))
	assert.True(t, seqRgxp.Match(rights[0].Sequence))
}
