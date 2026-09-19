package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tobilg/neoserver/testing/officialets/selection"
)

func main() {
	all := flag.Bool("all", false, "select every official suite")
	flag.Parse()
	var suites []string
	if *all {
		suites = selection.All()
	} else {
		paths := flag.Args()
		if len(paths) == 0 {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				paths = append(paths, scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		suites = selection.ForPaths(paths)
	}
	if suites == nil {
		suites = []string{}
	}
	document, err := json.Marshal(suites)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(document))
}
