package wfs

import (
	"os"
	"strconv"
	"time"
)

// Config holds the test configuration.
type Config struct {
	// BaseURL is the WFS service URL. If empty, mock server is used.
	BaseURL string

	// Timeout is the HTTP request timeout.
	Timeout time.Duration

	// Verbose enables verbose logging.
	Verbose bool

	// MaxFeatureTypes limits the number of feature types to test.
	MaxFeatureTypes int

	// ExhaustiveMode tests all combinations (slow).
	ExhaustiveMode bool
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() Config {
	cfg := Config{
		BaseURL:         os.Getenv("WFS_TEST_URL"),
		Timeout:         DefaultTimeout,
		Verbose:         false,
		MaxFeatureTypes: DefaultMaxTypes,
		ExhaustiveMode:  false,
	}

	if timeout := os.Getenv("WFS_TEST_TIMEOUT"); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			cfg.Timeout = d
		} else if secs, err := strconv.Atoi(timeout); err == nil {
			cfg.Timeout = time.Duration(secs) * time.Second
		}
	}

	if verbose := os.Getenv("WFS_TEST_VERBOSE"); verbose == "true" || verbose == "1" {
		cfg.Verbose = true
	}

	if max := os.Getenv("WFS_TEST_MAX_TYPES"); max != "" {
		if n, err := strconv.Atoi(max); err == nil && n > 0 {
			cfg.MaxFeatureTypes = n
		}
	}

	if exhaustive := os.Getenv("WFS_TEST_EXHAUSTIVE"); exhaustive == "true" || exhaustive == "1" {
		cfg.ExhaustiveMode = true
	}

	return cfg
}
