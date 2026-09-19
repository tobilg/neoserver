package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

func main() {
	manifestPath := flag.String("manifest", "testing/officialets/versions.lock.json", "manifest path")
	server := flag.String("server", "http://server:9000", "fixture server URL")
	flag.Parse()
	document, err := manifest.Load(*manifestPath)
	if err != nil {
		fail(err)
	}
	arguments := flag.Args()
	if len(arguments) == 0 {
		fail(fmt.Errorf("command is required"))
	}
	switch arguments[0] {
	case "suite":
		if len(arguments) != 2 {
			fail(fmt.Errorf("usage: suite SUITE"))
		}
		suite, ok := document.Suites[arguments[1]]
		if !ok {
			fail(fmt.Errorf("unknown suite %q", arguments[1]))
		}
		fmt.Printf("%s\t%s\n", suite.SuiteCode, suite.Image)
	case "profiles":
		if len(arguments) != 3 {
			fail(fmt.Errorf("usage: profiles EVIDENCE_KIND SUITE"))
		}
		for _, name := range document.ProfileNames(arguments[1], arguments[2]) {
			encoded, err := document.Arguments(name, *server)
			if err != nil {
				fail(err)
			}
			_, short, _ := strings.Cut(name, "/")
			fmt.Printf("%s\t%s\n", short, encoded)
		}
	case "derived":
		if len(arguments) != 2 {
			fail(fmt.Errorf("usage: derived PROFILE"))
		}
		profile, ok := document.Profiles[arguments[1]]
		if !ok || profile.EvidenceKind != "official-derived" {
			fail(fmt.Errorf("unknown derived profile %q", arguments[1]))
		}
		suite := document.Suites[profile.Suite]
		actualPatchSHA, err := manifest.FileSHA256(profile.Patch)
		if err != nil {
			fail(err)
		}
		if actualPatchSHA != profile.PatchSHA256 {
			fail(fmt.Errorf("derived ETS patch digest mismatch: got %s, want %s", actualPatchSHA, profile.PatchSHA256))
		}
		encoded, err := document.Arguments(arguments[1], *server)
		if err != nil {
			fail(err)
		}
		_, short, _ := strings.Cut(arguments[1], "/")
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", profile.Suite, short, suite.SuiteCode, suite.Image, profile.PatchSet, profile.Patch, profile.PatchSHA256, encoded)
	default:
		fail(fmt.Errorf("unknown command %q", arguments[0]))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
