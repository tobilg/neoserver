package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type runMetadata struct {
	Suite            string      `json:"suite"`
	SuiteCode        string      `json:"suite_code"`
	EvidenceKind     string      `json:"evidence_kind"`
	Image            string      `json:"image"`
	BaseImage        string      `json:"base_image,omitempty"`
	DerivedImageID   string      `json:"derived_image_id,omitempty"`
	PatchSet         string      `json:"patch_set,omitempty"`
	PatchSHA256      string      `json:"patch_sha256,omitempty"`
	Commit           string      `json:"neoserver_commit"`
	CandidateImageID string      `json:"neoserver_image_id,omitempty"`
	StartedAt        time.Time   `json:"started_at"`
	CompletedAt      time.Time   `json:"completed_at"`
	Arguments        url.Values  `json:"arguments"`
	Result           suiteResult `json:"result"`
	Error            string      `json:"error,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	suite := requiredEnv("ETS_SUITE")
	suiteCode := requiredEnv("ETS_SUITE_CODE")
	teamEngineURL := strings.TrimRight(requiredEnv("TEAMENGINE_URL"), "/")
	resultDir := requiredEnv("ETS_RESULT_DIR")
	if suite == "" || suiteCode == "" || teamEngineURL == "" || resultDir == "" {
		return errors.New("ETS_SUITE, ETS_SUITE_CODE, TEAMENGINE_URL and ETS_RESULT_DIR are required")
	}
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return fmt.Errorf("create result directory: %w", err)
	}

	arguments := make(url.Values)
	if raw := os.ExpandEnv(os.Getenv("ETS_ARGS_JSON")); raw != "" {
		var values map[string]string
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return fmt.Errorf("decode ETS_ARGS_JSON: %w", err)
		}
		for key, value := range values {
			arguments.Set(key, value)
		}
	}

	timeout := 45 * time.Minute
	if raw := os.Getenv("ETS_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("parse ETS_TIMEOUT: %w", err)
		}
		timeout = parsed
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := &http.Client{Timeout: timeout}
	user := firstNonEmpty(os.Getenv("TEAMENGINE_USER"), "ogctest")
	password := firstNonEmpty(os.Getenv("TEAMENGINE_PASSWORD"), "ogctest")

	metadata := runMetadata{
		Suite: suite, SuiteCode: suiteCode, EvidenceKind: firstNonEmpty(os.Getenv("ETS_EVIDENCE_KIND"), "official"),
		Image: os.Getenv("ETS_IMAGE"), BaseImage: os.Getenv("ETS_BASE_IMAGE"), DerivedImageID: os.Getenv("ETS_DERIVED_IMAGE_ID"),
		PatchSet: os.Getenv("ETS_PATCH_SET"), PatchSHA256: os.Getenv("ETS_PATCH_SHA256"), Commit: os.Getenv("NEOSERVER_COMMIT"), CandidateImageID: os.Getenv("NEOSERVER_IMAGE_ID"),
		StartedAt: time.Now().UTC(), Arguments: arguments,
	}
	if err := validateEvidenceProvenance(metadata); err != nil {
		return err
	}
	defer func() {
		metadata.CompletedAt = time.Now().UTC()
		if runErr != nil {
			metadata.Error = runErr.Error()
		}
		if err := writeJSON(filepath.Join(resultDir, "metadata.json"), metadata); err != nil && runErr == nil {
			runErr = fmt.Errorf("write ETS metadata: %w", err)
		}
		if err := writeJUnit(filepath.Join(resultDir, "junit.xml"), metadata, runErr); err != nil && runErr == nil {
			runErr = fmt.Errorf("write ETS JUnit result: %w", err)
		}
	}()

	if runErr = waitForTEAMEngine(ctx, client, teamEngineURL, user, password); runErr != nil {
		return runErr
	}
	runURL := teamEngineURL + "/rest/suites/" + url.PathEscape(suiteCode) + "/run"
	if encoded := arguments.Encode(); encoded != "" {
		runURL += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runURL, nil)
	if err != nil {
		runErr = err
		return runErr
	}
	req.SetBasicAuth(user, password)
	req.Header.Set("Accept", "application/xml")
	response, err := client.Do(req)
	if err != nil {
		runErr = fmt.Errorf("execute official %s ETS: %w", suite, err)
		return runErr
	}
	defer response.Body.Close()
	document, err := io.ReadAll(response.Body)
	if err != nil {
		runErr = fmt.Errorf("read official %s ETS result: %w", suite, err)
		return runErr
	}
	if err := os.WriteFile(filepath.Join(resultDir, "result.xml"), document, 0o644); err != nil {
		runErr = fmt.Errorf("write raw ETS result: %w", err)
		return runErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		runErr = fmt.Errorf("TEAM Engine returned %s", response.Status)
		return runErr
	}
	metadata.Result, runErr = parseSuiteResult(document)
	if runErr != nil {
		return runErr
	}
	fmt.Printf("%s %s ETS passed: %d leaf assertions passed, %d skipped, %d total\n", metadata.EvidenceKind, suite, metadata.Result.Passed, metadata.Result.Skipped, metadata.Result.Total)
	return nil
}

func validateEvidenceProvenance(metadata runMetadata) error {
	switch metadata.EvidenceKind {
	case "official":
		if metadata.BaseImage != "" || metadata.DerivedImageID != "" || metadata.PatchSet != "" || metadata.PatchSHA256 != "" {
			return errors.New("stock official evidence must not contain derived-image or patch provenance")
		}
	case "official-derived":
		if metadata.BaseImage == "" || metadata.DerivedImageID == "" || metadata.PatchSet == "" || metadata.PatchSHA256 == "" {
			return errors.New("official-derived evidence requires base image, derived image ID, patch set, and patch SHA-256")
		}
	default:
		return fmt.Errorf("unsupported ETS evidence kind %q", metadata.EvidenceKind)
	}
	return nil
}

func waitForTEAMEngine(ctx context.Context, client *http.Client, baseURL, user, password string) error {
	endpoint := baseURL + "/rest/suites"
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(user, password)
		response, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for TEAM Engine: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func writeJSON(path string, value any) error {
	document, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	document = append(document, '\n')
	return os.WriteFile(path, document, 0o644)
}

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	TestCase []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name    string        `xml:"name,attr"`
	Class   string        `xml:"classname,attr"`
	Failure *junitMessage `xml:"failure,omitempty"`
	Skipped *junitMessage `xml:"skipped,omitempty"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr,omitempty"`
}

func writeJUnit(path string, metadata runMetadata, runErr error) error {
	suite := junitSuite{Name: metadata.EvidenceKind + "-ets-" + metadata.Suite}
	hasFailure := false
	for _, result := range metadata.Result.Cases {
		className := result.Class
		if className == "" {
			className = "conformance." + metadata.Suite
		}
		if result.Kind != "" {
			className = result.Kind + "." + className
		}
		item := junitCase{Name: result.Name, Class: className}
		switch result.Status {
		case "failed":
			item.Failure = &junitMessage{Message: result.Message}
			suite.Failures++
			hasFailure = true
		case "skipped":
			item.Skipped = &junitMessage{Message: result.Message, Type: result.SkipCategory}
			suite.Skipped++
		}
		suite.TestCase = append(suite.TestCase, item)
	}
	if runErr != nil && !hasFailure {
		suite.Failures++
		suite.TestCase = append(suite.TestCase, junitCase{
			Name: "TEAM Engine controller", Class: "infrastructure.conformance." + metadata.Suite,
			Failure: &junitMessage{Message: runErr.Error()},
		})
	}
	if len(suite.TestCase) == 0 {
		suite.TestCase = append(suite.TestCase, junitCase{Name: "TEAM Engine result", Class: "conformance." + metadata.Suite})
	}
	suite.Tests = len(suite.TestCase)
	document, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	document = append([]byte(xml.Header), document...)
	document = append(document, '\n')
	return os.WriteFile(path, document, 0o644)
}

func requiredEnv(name string) string { return strings.TrimSpace(os.Getenv(name)) }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
