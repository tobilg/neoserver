package main

import (
	"fmt"
	"os"

	"github.com/tobilg/neoserver/internal/conf"
)

func mustStoreConfig(configFile, pathOverride string) conf.Store {
	cfg, err := conf.LoadStore(configFile, pathOverride)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	return cfg
}
