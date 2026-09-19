// Package ogcapi provides conformance tests for OGC API - Features.
// It implements tests based on the OGC API - Features 1.0 Conformance Test Suite.
package ogcapi

import (
	"os"
	"strconv"
	"time"
)

// Config holds test configuration loaded from environment variables.
type Config struct {
	// BaseURL is the root URL of the OGC API Features server (e.g., "http://localhost:9000/ogc")
	BaseURL string

	// NoOfCollections limits how many collections to test (0 = all)
	NoOfCollections int

	// FeaturesLimit limits how many features to retrieve per collection
	FeaturesLimit int

	// Timeout is the HTTP request timeout
	Timeout time.Duration

	// Verbose enables verbose logging
	Verbose bool
}

// LoadConfig loads test configuration from environment variables.
func LoadConfig() Config {
	return Config{
		BaseURL:         envOr("OGC_TEST_URL", "http://localhost:9000/ogc"),
		NoOfCollections: envIntOr("OGC_TEST_COLLECTIONS", 0),
		FeaturesLimit:   envIntOr("OGC_TEST_FEATURES_LIMIT", 100),
		Timeout:         envDurationOr("OGC_TEST_TIMEOUT", 30*time.Second),
		Verbose:         envBoolOr("OGC_TEST_VERBOSE", false),
	}
}

// envOr returns the environment variable value or a default.
func envOr(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// envIntOr returns the environment variable as int or a default.
func envIntOr(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

// envBoolOr returns the environment variable as bool or a default.
func envBoolOr(key string, defaultValue bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultValue
}

// envDurationOr returns the environment variable as duration or a default.
func envDurationOr(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultValue
}
