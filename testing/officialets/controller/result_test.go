package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTestNGResult(t *testing.T) {
	document := []byte(`<testng-results total="3" passed="1" failed="0" skipped="2">
<suite><test name="core"><class name="example.Core">
<test-method status="PASS" name="passes" description="requirement"/>
<test-method status="SKIP" name="skips"/>
<test-method status="SKIP" name="setup" is-config="true"/>
</class></test></suite></testng-results>`)
	result, err := parseSuiteResult(document)
	if err != nil || result.Passed != 1 || result.Skipped != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Leaf.Total != 2 || result.Infrastructure.Total != 1 || len(result.Cases) != 3 {
		t.Fatalf("unexpected classified result: %+v", result)
	}
	if result.SkipCategories["unclassified"] != 1 || result.SkipCategories["infrastructure"] != 1 {
		t.Fatalf("unexpected skip categories: %+v", result.SkipCategories)
	}
}

func TestParseTestNGResultFailsClosed(t *testing.T) {
	for _, document := range []string{
		`<testng-results total="8" failed="1" skipped="0"></testng-results>`,
		`<testng-results total="0" failed="0" skipped="0"></testng-results>`,
		`<testng-results total="3" failed="0" skipped="3"></testng-results>`,
	} {
		if _, err := parseSuiteResult([]byte(document)); err == nil {
			t.Fatalf("expected failure for %s", document)
		}
	}
}

func TestParseCTLResultClassifiesLeavesAndWrappers(t *testing.T) {
	document := []byte(`<execution>
<starttest local-name="root" file="suite.xml"/>
  <starttest local-name="pass" file="leaf.xml"/><endtest result="1"/>
  <starttest local-name="skip" file="leaf.xml"/><endtest result="Skipped"/>
<endtest result="1"/>
</execution>`)
	result, err := parseSuiteResult(document)
	if err != nil {
		t.Fatal(err)
	}
	if result.Leaf.Passed != 1 || result.Leaf.Skipped != 1 || result.Wrapper.Passed != 1 || len(result.Cases) != 2 {
		t.Fatalf("unexpected CTL classification: %+v", result)
	}
}

func TestParseCTLLeafFailureIsNotDoubleCounted(t *testing.T) {
	document := []byte(`<execution>
<starttest local-name="root"/><starttest local-name="group"/><starttest local-name="leaf"/>
<endtest result="6"/><endtest result="5"/><endtest result="5"/>
</execution>`)
	result, err := parseSuiteResult(document)
	if err == nil {
		t.Fatal("expected failed CTL result")
	}
	if result.Leaf.Failed != 1 || result.Wrapper.Failed != 2 || result.Failed != 1 {
		t.Fatalf("propagated failures were not classified: %+v", result)
	}
}

func TestParseCTLFlatPathReferencesClassifiesWrappers(t *testing.T) {
	document := []byte(`<execution>
<log><starttest local-name="root" path="suite/root"/><testcall path="suite/root/group"/><endtest result="5"/></log>
<log><starttest local-name="group" path="suite/root/group"/><testcall path="suite/root/group/leaf"/><endtest result="5"/></log>
<log><starttest local-name="leaf" path="suite/root/group/leaf"/><endtest result="6"/></log>
</execution>`)
	result, err := parseSuiteResult(document)
	if err == nil {
		t.Fatal("expected failed CTL result")
	}
	if result.Leaf.Failed != 1 || result.Wrapper.Failed != 2 || result.Failed != 1 || len(result.Cases) != 1 {
		t.Fatalf("flat CTL paths were not classified: %+v", result)
	}
}

func TestParseCTLUnknownResultIsInfrastructureFailure(t *testing.T) {
	document := []byte(`<execution><starttest local-name="root"/><starttest local-name="leaf"/><endtest result="-1"/><endtest result="5"/></execution>`)
	result, err := parseSuiteResult(document)
	if err == nil || !strings.Contains(err.Error(), `unknown CTL test result "-1"`) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Infrastructure.Failed != 1 || len(result.Cases) != 1 || result.Cases[0].Kind != "infrastructure" {
		t.Fatalf("unknown status was not classified as infrastructure: %+v", result)
	}
}

func TestParseCTLResultFailsClosed(t *testing.T) {
	for _, document := range []string{
		`<execution><starttest local-name="only"/><endtest result="3"/></execution>`,
		`<execution></execution>`,
		`<execution><endtest result="1"/></execution>`,
		`<html></html>`,
	} {
		if _, err := parseSuiteResult([]byte(document)); err == nil {
			t.Fatalf("expected failure for %s", document)
		}
	}
}

func TestWriteJUnitEmitsClassifiedCases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junit.xml")
	metadata := runMetadata{Suite: "sample", EvidenceKind: "official", Result: suiteResult{Cases: []caseResult{
		{Name: "pass", Class: "core", Kind: "assertion", Status: "passed"},
		{Name: "skip", Class: "core", Kind: "assertion", Status: "skipped", Message: "not applicable", SkipCategory: "fixture-not-applicable"},
	}}}
	if err := writeJUnit(path, metadata, nil); err != nil {
		t.Fatal(err)
	}
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`tests="2"`, `skipped="1"`, `name="pass"`, `name="skip"`, `type="fixture-not-applicable"`} {
		if !strings.Contains(string(document), expected) {
			t.Errorf("JUnit is missing %s: %s", expected, document)
		}
	}
}

func TestSkipCategory(t *testing.T) {
	for name, test := range map[string]struct {
		item caseResult
		want string
	}{
		"fixture":            {caseResult{Name: "no temporal properties available"}, "fixture-not-applicable"},
		"conditional":        {caseResult{Name: "TileMatrixSetLimits"}, "conditional-protocol-branch"},
		"conditional group":  {caseResult{Name: "Server.KVP.GET.GetTile.Optional"}, "conditional-protocol-branch"},
		"optional":           {caseResult{Message: "spatial joins are not advertised"}, "unclaimed-optional-capability"},
		"dependent optional": {caseResult{Class: "org.opengis.cite.iso19142.versioning.VersioningTests", Message: "See OGC 09-025: 15.3.5"}, "unclaimed-optional-capability"},
		"nillable fixture":   {caseResult{Message: "FeatureType places does not contain at least one nillable property"}, "fixture-not-applicable"},
		"version":            {caseResult{Name: "WFS 2.0.2 only"}, "profile-version"},
		"unknown":            {caseResult{Name: "opaque CTL branch"}, "unclassified"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := skipCategory(test.item); got != test.want {
				t.Fatalf("category = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateEvidenceProvenance(t *testing.T) {
	validDerived := runMetadata{
		EvidenceKind: "official-derived", BaseImage: "base@sha256:digest", DerivedImageID: "sha256:image",
		PatchSet: "patch-v1", PatchSHA256: strings.Repeat("a", 64),
	}
	for name, metadata := range map[string]runMetadata{
		"stock":   {EvidenceKind: "official"},
		"derived": validDerived,
	} {
		if err := validateEvidenceProvenance(metadata); err != nil {
			t.Errorf("%s provenance: %v", name, err)
		}
	}
	for name, metadata := range map[string]runMetadata{
		"stock with patch": {EvidenceKind: "official", PatchSet: "hidden"},
		"derived no id":    {EvidenceKind: "official-derived", BaseImage: validDerived.BaseImage, PatchSet: validDerived.PatchSet, PatchSHA256: validDerived.PatchSHA256},
		"unknown":          {EvidenceKind: "ported"},
	} {
		if err := validateEvidenceProvenance(metadata); err == nil {
			t.Errorf("%s provenance unexpectedly accepted", name)
		}
	}
}
