// Package seqio is a generic namespace shared by all biological sequence input and output
// handlers.
// This package contain a very barebone and simple Fasta format sequence file parser.
// Currently, it parses and returns the Id(header) and sequence.
// This is mostly working concept, however could be easily extended in future.
// Example
//
//	package main
//
//	import (
//		"fmt"
//		"github.com/dictybase-docker/interpro-manager/internal/seqio"
//		"os"
//		"log"
//	)
//
//	func main() {
//		r := seqio.NewFastaReader(os.Args[1])
//		for r.HasEntry() {
//			fasta := r.NextEntry()
//			fmt.Printf("id:%s\nSequence:%s\n", f.ID, f.Sequence)
//		}
//	}
package seqio

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"os"
)

// Fasta is a struct representing a fasta record, containing the sequence ID and
// the sequence itself.
type Fasta struct {
	ID       []byte // sequence id or header immediately followed by ">" symbol
	Sequence []byte // The entire sequence
}

// FastaReader is a type for reading fasta files. It maintains the state of the reader and the
// current fasta record being processed.
type FastaReader struct {
	reader     *bufio.Reader // pointer to a buffered reader
	seenHeader bool
	header     []byte
	sequence   []byte
	exhausted  bool
	fasta      *Fasta
}

// NewFastaReader initializes a new FastaReader for the given file path. It
// opens the file and creates a buffered reader for efficient reading. If the
// file cannot be opened, it logs a fatal error.
func NewFastaReader(file string) *FastaReader {
	reader, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}
	return &FastaReader{
		reader: bufio.NewReader(reader),
	}
}

// NextEntry returns the next fasta entry as a Fasta struct.
func (f *FastaReader) NextEntry() *Fasta {
	return f.fasta
}

// HasEntry checks if there is another fasta entry to read.
func (f *FastaReader) HasEntry() bool {
	for {
		line, err := f.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				if !f.exhausted {
					f.exhausted = true
					f.fasta = &Fasta{ID: f.header, Sequence: f.sequence}
					return true
				}
				return false
			}
			log.Fatal(err)
		}
		if bytes.HasPrefix(line, []byte(">")) {
			if !f.seenHeader {
				f.header = line[1 : len(line)-1]
				f.seenHeader = true
			} else {
				f.fasta = &Fasta{ID: f.header, Sequence: f.sequence}
				f.header = line[1 : len(line)-1]
				f.sequence = []byte{}
				return true
			}
		} else {
			f.sequence = append(f.sequence, bytes.TrimSuffix(line, []byte("\n"))...)
		}
	}
}
