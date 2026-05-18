package seqio

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	headerRgxp = regexp.MustCompile(`^\w{1,4}`)
	seqRgxp    = regexp.MustCompile(`^[A-Z]+$`)
)

func TestSingleFasta(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err, "Could not get current directory")

	mfasta := filepath.Join(dir, "testdata", "single.fa")
	reader := NewFastaReader(mfasta)
	require.True(t, reader.HasEntry(), "Did not get expected iteration")
	entry := reader.NextEntry()
	require.NotNil(t, entry, "Did not get expected entry")

	assert.True(t, bytes.HasPrefix(entry.ID, []byte("tr|Q95Q25")), "Expected to match header")
	assert.True(t, seqRgxp.Match(entry.Sequence), "Expected to match sequence")
}

func TestMultiFasta(t *testing.T) {
	dir, err := os.Getwd()
	require.NoError(t, err, "Could not get current directory")

	mfasta := filepath.Join(dir, "testdata", "multi.fa")
	reader := NewFastaReader(mfasta)
	for i := 0; i <= 3; i++ {
		require.True(t, reader.HasEntry(), "Did not get expected iteration")
		entry := reader.NextEntry()
		require.NotNil(t, entry, "Did not get expected entry")

		assert.True(t, headerRgxp.Match(entry.ID), "Expected to match header")
		assert.True(t, seqRgxp.Match(entry.Sequence), "Expected to match sequence")
	}
}
