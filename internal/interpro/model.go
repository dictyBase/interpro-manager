package interpro

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
