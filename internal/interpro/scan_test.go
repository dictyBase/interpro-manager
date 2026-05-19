package interpro

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	E "github.com/IBM/fp-go/v2/either"
	F "github.com/IBM/fp-go/v2/function"
	IOE "github.com/IBM/fp-go/v2/ioeither"
	ioehttp "github.com/IBM/fp-go/v2/ioeither/http"
	T "github.com/IBM/fp-go/v2/tuple"

	"github.com/dictybase-docker/interpro-manager/internal/seqio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func isRightScan[A any](e E.Either[error, A]) bool { return E.IsRight(e) }

func unwrapRightScan[A any](e E.Either[error, A]) A {
	return E.Fold(func(err error) A { panic(err) }, F.Identity[A])(e)
}

func unwrapLeftScan[A any](e E.Either[error, A]) error {
	return E.Fold[error, A](F.Identity[error], func(_ A) error { panic("expected Left") })(e)
}

func scanConfig(baseURL string) ScanRequest {
	return ScanRequest{
		Email:        "test@ebi.ac.uk",
		SeqType:      "p",
		BaseURL:      baseURL,
		PollInterval: 1 * time.Second,
		Timeout:      5 * time.Second,
		OutputDir:    "",
	}
}

func TestExtractScanRequest(t *testing.T) {
	t.Run("all fields mapped from flags", func(t *testing.T) {
		run := false
		cmd := &cli.Command{
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "fasta"},
				&cli.StringFlag{Name: "email"},
				&cli.StringFlag{Name: "output", Value: "."},
				&cli.StringFlag{Name: "seq-type", Value: "p"},
				&cli.DurationFlag{Name: "poll-interval", Value: 15 * time.Second},
				&cli.DurationFlag{Name: "timeout", Value: 30 * time.Minute},
			},
			Action: func(_ context.Context, c *cli.Command) error {
				run = true
				r := extractScanRequest(c)
				assert.Equal(t, "test.fa", r.FastaPath)
				assert.Equal(t, "a@b.com", r.Email)
				assert.Equal(t, "/tmp/out", r.OutputDir)
				assert.Equal(t, "n", r.SeqType)
				assert.Equal(t, scanBaseURL, r.BaseURL)
				assert.Equal(t, 5*time.Second, r.PollInterval)
				assert.Equal(t, 10*time.Minute, r.Timeout)
				return nil
			},
		}
		err := cmd.Run(context.Background(), []string{
			"app", "scan",
			"--fasta", "test.fa",
			"--email", "a@b.com",
			"--output", "/tmp/out",
			"--seq-type", "n",
			"--poll-interval", "5s",
			"--timeout", "10m",
		})
		require.NoError(t, err)
		assert.True(t, run)
	})
}

func TestValidateScanRequest(t *testing.T) {
	t.Run("missing email", func(t *testing.T) {
		result := validateScanRequest(ScanRequest{FastaPath: "x.fa"})
		require.True(t, E.IsLeft(result))
		assert.Contains(t, unwrapLeftScan(result).Error(), "email is required")
	})

	t.Run("missing fasta path", func(t *testing.T) {
		result := validateScanRequest(ScanRequest{Email: "a@b.com"})
		require.True(t, E.IsLeft(result))
		assert.Contains(t, unwrapLeftScan(result).Error(), "fasta path is required")
	})

	t.Run("file not found", func(t *testing.T) {
		result := validateScanRequest(ScanRequest{
			Email:     "a@b.com",
			FastaPath: "/nonexistent/path.fa",
		})
		require.True(t, E.IsLeft(result))
		assert.Contains(t, unwrapLeftScan(result).Error(), "fasta file not found")
	})

	t.Run("valid request", func(t *testing.T) {
		dir, err := os.Getwd()
		require.NoError(t, err)
		fastaPath := filepath.Join(dir, "..", "seqio", "testdata", "single.fa")

		result := validateScanRequest(ScanRequest{
			Email:     "a@b.com",
			FastaPath: fastaPath,
		})
		require.True(t, isRightScan(result))
	})
}

func TestBuildSubmitRequester(t *testing.T) {
	config := ScanRequest{
		Email:   "tester@example.com",
		SeqType: "p",
		BaseURL: "http://test.local",
	}

	rec := seqio.Fasta{
		ID:       []byte("tr|Q95Q25|ANMK_CAEEL Adenylate kinase"),
		Sequence: []byte("MGSTESTSEQUENCE"),
	}

	reqEither := buildSubmitRequester(config, rec)()
	require.True(t, isRightScan(reqEither))
	req := unwrapRightScan(reqEither)

	assert.Equal(t, "POST", req.Method)
	assert.Contains(t, req.URL.String(), "/run")
	assert.Contains(t, req.URL.String(), "test.local")

	assert.Equal(t, "application/x-www-form-urlencoded", req.Header.Get("Content-Type"))
	assert.Equal(t, "text/plain", req.Header.Get("Accept"))

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body.Close()
	bodyStr := string(body)

	assert.Contains(t, bodyStr, "email=tester%40example.com")
	assert.Contains(t, bodyStr, "stype=p")
	assert.Contains(t, bodyStr, "goterms=true")
	assert.Contains(t, bodyStr, "pathways=true")
	assert.Contains(t, bodyStr, "sequence=%3Etr%7CQ95Q25%7CANMK_CAEEL+Adenylate+kinase")
}

func TestSaveResult(t *testing.T) {
	tmpDir := t.TempDir()

	job := CompletedJob{
		JobID:  "job-123",
		SeqID:  "tr|Q95Q25",
		Config: ScanRequest{OutputDir: tmpDir},
	}

	result := saveResult(job)(`{"results": [{"accession": "IPR000001"}]}`)()
	require.True(t, isRightScan(result))

	outputPath := unwrapRightScan(result)
	assert.Contains(t, outputPath, "tr|Q95Q25")
	assert.Contains(t, outputPath, "job-123")
	assert.Contains(t, outputPath, ".json")

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.JSONEq(t, `{"results": [{"accession": "IPR000001"}]}`, string(content))
}

func TestExtractSeqID(t *testing.T) {
	t.Run("first token extracted", func(t *testing.T) {
		rec := seqio.Fasta{
			ID: []byte("tr|Q95Q25|ANMK_CAEEL Adenylate kinase"),
		}
		assert.Equal(t, "tr|Q95Q25|ANMK_CAEEL", extractSeqID(rec))
	})

	t.Run("no whitespace", func(t *testing.T) {
		rec := seqio.Fasta{ID: []byte("simple_id")}
		assert.Equal(t, "simple_id", extractSeqID(rec))
	})
}

func TestBuildSubmitRequesterBody(t *testing.T) {
	config := ScanRequest{
		Email:   "test@ebi.ac.uk",
		SeqType: "p",
		BaseURL: "http://test.local",
	}

	rec := seqio.Fasta{
		ID:       []byte("test_seq"),
		Sequence: []byte("MKFLVLALL"),
	}

	reqEither := buildSubmitRequester(config, rec)()
	require.True(t, isRightScan(reqEither))
	req := unwrapRightScan(reqEither)

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body.Close()
	bodyStr := string(body)

	assert.Contains(t, bodyStr, fmt.Sprintf("email=%s", "test%40ebi.ac.uk"))
	assert.Contains(t, bodyStr, "stype=p")
	assert.Contains(t, bodyStr, "sequence=%3Etest_seq%0AMKFLVLALL")
}

func TestEnsureOutputDir(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "new-subdir")

	result := ensureOutputDir(tmpDir)()
	require.True(t, isRightScan(result))

	info, err := os.Stat(tmpDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestSubmitOneRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "JOB-ABC-123")
	}))
	defer server.Close()

	config := scanConfig(server.URL)
	args := SubmitArgs{F1: ioehttp.MakeClient(server.Client()), F2: config}

	rec := seqio.Fasta{
		ID:       []byte("test_seq"),
		Sequence: []byte("MKFLVLALL"),
	}

	result := submitOneRecord(args)(rec)()
	require.True(t, isRightScan(result))

	job := unwrapRightScan(result)
	assert.Equal(t, "JOB-ABC-123", job.JobID)
	assert.Equal(t, "test_seq", job.SeqID)
}

func TestSubmitOneRecordServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := scanConfig(server.URL)
	args := SubmitArgs{F1: ioehttp.MakeClient(server.Client()), F2: config}

	rec := seqio.Fasta{
		ID:       []byte("test_seq"),
		Sequence: []byte("MKFLVLALL"),
	}

	result := submitOneRecord(args)(rec)()
	require.True(t, E.IsLeft(result))
}

func TestGetJobStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "RUNNING")
	}))
	defer server.Close()

	client := ioehttp.MakeClient(server.Client())

	result := getJobStatus(client, server.URL, "JOB-123")()
	require.True(t, isRightScan(result))
	assert.Equal(t, "RUNNING", unwrapRightScan(result))
}

func TestDownloadJSONResult(t *testing.T) {
	const jsonResponse = `{"results":[{"metadata":{"accession":"A1","name":"Protein 1"}}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, jsonResponse)
	}))
	defer server.Close()

	job := CompletedJob{
		JobID:  "JOB-123",
		SeqID:  "test_seq",
		Client: ioehttp.MakeClient(server.Client()),
		Config: scanConfig(server.URL),
	}

	result := downloadJSONResult(job)()
	require.True(t, isRightScan(result))
	assert.JSONEq(t, jsonResponse, unwrapRightScan(result))
}

func TestPollJobFinished(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "FINISHED")
	}))
	defer server.Close()

	job := SubmittedJob{
		JobID:  "JOB-DONE",
		SeqID:  "seq1",
		Client: ioehttp.MakeClient(server.Client()),
		Config: scanConfig(server.URL),
	}

	result := pollJob(job)()
	require.True(t, isRightScan(result))

	completed := unwrapRightScan(result)
	assert.Equal(t, "JOB-DONE", completed.JobID)
	assert.Equal(t, "seq1", completed.SeqID)
}

func TestPollJobTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "RUNNING")
	}))
	defer server.Close()

	config := scanConfig(server.URL)
	config.Timeout = 50 * time.Millisecond
	config.PollInterval = 100 * time.Millisecond

	job := SubmittedJob{
		JobID:  "JOB-TIMEOUT",
		SeqID:  "seq1",
		Client: ioehttp.MakeClient(server.Client()),
		Config: config,
	}

	result := pollJob(job)()
	require.True(t, E.IsLeft(result))
	assert.Contains(t, unwrapLeftScan(result).Error(), "timed out")
}

func TestPollJobErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "FAILED")
	}))
	defer server.Close()

	config := scanConfig(server.URL)
	config.PollInterval = 10 * time.Millisecond

	job := SubmittedJob{
		JobID:  "JOB-FAIL",
		SeqID:  "seq1",
		Client: ioehttp.MakeClient(server.Client()),
		Config: config,
	}

	result := pollJob(job)()
	require.True(t, E.IsLeft(result))
	assert.Contains(t, unwrapLeftScan(result).Error(), "ended with status: FAILED")
}

func TestProcessOneFastaIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		path := r.URL.Path

		switch {
		case strings.HasSuffix(path, "/run"):
			_, _ = io.WriteString(w, "JOB-INT-1")
		case strings.Contains(path, "/status/"):
			_, _ = io.WriteString(w, "FINISHED")
		case strings.Contains(path, "/result/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"results":[{"metadata":{"accession":"A1"}}]}`)
		}
	}))
	defer server.Close()

	config := scanConfig(server.URL)
	config.PollInterval = 10 * time.Millisecond
	config.OutputDir = t.TempDir()

	args := T.MakeTuple2[ioehttp.Client, ScanRequest](
		ioehttp.MakeClient(server.Client()),
		config,
	)

	rec := seqio.Fasta{
		ID:       []byte("test_seq"),
		Sequence: []byte("MKFLVLALL"),
	}

	result := F.Pipe3(
		submitOneRecord(args)(rec),
		IOE.Chain(pollJob),
		IOE.Chain(downloadAndSave),
		toEither[error, string],
	)
	require.True(t, isRightScan(result))

	outputPath := unwrapRightScan(result)
	assert.Contains(t, outputPath, ".json")

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.NotEmpty(t, content)
}

func TestStreamFastaRecordsEndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		path := r.URL.Path

		switch {
		case strings.HasSuffix(path, "/run"):
			_, _ = io.WriteString(w, "JOB-EOF-1")
		case strings.Contains(path, "/status/"):
			_, _ = io.WriteString(w, "FINISHED")
		case strings.Contains(path, "/result/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"results":[]}`)
		}
	}))
	defer server.Close()

	dir, err := os.Getwd()
	require.NoError(t, err)

	config := scanConfig(server.URL)
	config.PollInterval = 10 * time.Millisecond
	config.OutputDir = t.TempDir()
	config.FastaPath = filepath.Join(dir, "..", "seqio", "testdata", "multi.fa")

	args := T.MakeTuple2[ioehttp.Client, ScanRequest](
		ioehttp.MakeClient(server.Client()),
		config,
	)

	_ = ensureOutputDir(config.OutputDir)()

	result := streamFastaRecords(args)()
	require.True(t, isRightScan(result))

	paths := unwrapRightScan(result)
	assert.Len(t, paths, 4)

	for _, p := range paths {
		assert.Contains(t, p, ".json")
		content, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.NotEmpty(t, content)
	}
}
