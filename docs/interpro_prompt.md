# InterPro CLI Development Prompt

Construct a Golang CLI application for interacting with the InterPro protein database, adhering to strict functional programming principles.

## Objective
Implement a subcommand that extracts protein metadata for *Dictyostelium discoideum* (Taxonomy ID: 44689) and saves it to a TSV file.

## Functional Requirements
1.  **Data Source**: Query the InterPro API at: `https://www.ebi.ac.uk/interpro/api/protein/UniProt/taxonomy/uniprot/44689/?page_size=20`
2.  **Pagination**: Implement recursive iteration by following the `next` URL in the JSON response until it is null.
3.  **Data Extraction**: From each result in `results[]`, extract:
    *   `metadata.accession` (UniProt ID)
    *   `metadata.name` (Protein Name)
    *   `metadata.gene` (Gene ID/Name)
4.  **Filtering**: Skip entries where the `gene` field is missing or empty.
5.  **Output**: Write the collected records to a TSV file.

## Technical Architecture
1.  **Framework**: Use `urfave/cli/v3`.
2.  **Paradigm**: Pure functional style using `fp-go/v2`.
3.  **Core Libraries**:
    *   `github.com/IBM/fp-go/v2`
    *   Utilize the `effect` package for managed side effects.
    *   Mix with `IOEither`, `Either`, `Option`, `Predicate`, `Eq`, and `Array` packages.
4.  **Implementation Patterns**:
    *   Refer to `fp-go-concepts/v2/http` for HTTP/JSON handling.
    *   Refer to `modware-import` (specifically the `gpad` subcommand) for streaming/paginated download patterns.
    *   Architect the solution following the modularity and structure demonstrated in `crush-sandbox`.
5.  **Side Effect Isolation**: All non-pure operations (network, file I/O) must be encapsulated within `IOEither`. Use `F.Pipe` for all transformations.

## Reference Data
The API response structure is defined in `docs/interpro_api_response.json`. Ensure the JSON unmarshaling logic matches this schema precisely.
