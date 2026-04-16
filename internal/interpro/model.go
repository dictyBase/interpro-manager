package interpro

import (
	"os"

	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	T "github.com/IBM/fp-go/v2/tuple"
)

type SourceOrganism struct {
	TaxID          string `json:"taxId"`
	ScientificName string `json:"scientificName"`
	FullName       string `json:"fullName"`
}

type Metadata struct {
	Accession      string         `json:"accession"`
	Name           string         `json:"name"`
	SourceDatabase string         `json:"source_database"`
	Length         int            `json:"length"`
	SourceOrganism SourceOrganism `json:"source_organism"`
	Gene           string         `json:"gene"`
	InAlphafold    bool           `json:"in_alphafold"`
	InBfvd         bool           `json:"in_bfvd"`
}

type Taxon struct {
	Accession      string   `json:"accession"`
	Lineage        []string `json:"lineage"`
	SourceDatabase string   `json:"source_database"`
}

type Result struct {
	Metadata Metadata `json:"metadata"`
	Taxa     []Taxon  `json:"taxa"`
}

type APIResponse struct {
	Count    int      `json:"count"`
	Next     *string  `json:"next"`
	Previous *string  `json:"previous"`
	Results  []Result `json:"results"`
}

type ProteinRecord struct {
	Accession string
	Name      string
	Gene      string
}

type ExtractConfig = T.Tuple3[ioehttp.Client, string, string]

type RuntimeState = T.Tuple4[ioehttp.Client, string, string, *os.File]

type PageData = T.Tuple3[APIResponse, []ProteinRecord, string]

type PageStep = T.Tuple4[error, string, []ProteinRecord, string]

type LoopStep = T.Tuple3[error, string, string]

func configOutputPath(cfg ExtractConfig) string { return cfg.F3 }

func runtimeClient(state RuntimeState) ioehttp.Client { return state.F1 }
func runtimeURL(state RuntimeState) string            { return state.F2 }
func runtimeOutputPath(state RuntimeState) string     { return state.F3 }
func runtimeHandle(state RuntimeState) *os.File       { return state.F4 }

func pageRows(data PageData) []ProteinRecord { return data.F2 }
func pageNext(data PageData) string          { return data.F3 }

func stepError(step PageStep) error  { return step.F1 }
func stepChunk(step PageStep) string { return step.F2 }
func stepNext(step PageStep) string  { return step.F4 }

func loopError(step LoopStep) error   { return step.F1 }
func loopNext(step LoopStep) string   { return step.F2 }
func loopOutput(step LoopStep) string { return step.F3 }
