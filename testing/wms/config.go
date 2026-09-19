package wms

import (
	"os"
	"strconv"
	"time"
)

// Config holds test configuration loaded from environment variables.
type Config struct {
	// BaseURL is the WMS endpoint URL (e.g., http://localhost:9000/wms).
	BaseURL string

	// Timeout is the HTTP request timeout.
	Timeout time.Duration

	// Verbose enables verbose logging during tests.
	Verbose bool

	// MaxLayers limits the number of layers tested in exhaustive mode.
	MaxLayers int

	// ExhaustiveMode runs all layer/CRS combinations (slower but more thorough).
	ExhaustiveMode bool
}

// LoadConfig loads test configuration from environment variables.
// Environment variables:
//   - WMS_TEST_URL: Base URL of WMS server (default: uses mock server)
//   - WMS_TEST_TIMEOUT: Request timeout in seconds (default: 30)
//   - WMS_TEST_VERBOSE: Enable verbose output (default: false)
//   - WMS_TEST_MAX_LAYERS: Max layers for exhaustive tests (default: 10)
//   - WMS_TEST_EXHAUSTIVE: Run exhaustive tests (default: false)
func LoadConfig() Config {
	return Config{
		BaseURL:        envOr("WMS_TEST_URL", ""),
		Timeout:        envDurationOr("WMS_TEST_TIMEOUT", DefaultTimeout*time.Second),
		Verbose:        envBoolOr("WMS_TEST_VERBOSE", false),
		MaxLayers:      envIntOr("WMS_TEST_MAX_LAYERS", DefaultMaxLayers),
		ExhaustiveMode: envBoolOr("WMS_TEST_EXHAUSTIVE", false),
	}
}

// envOr returns the value of the environment variable or the default.
func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envIntOr returns the integer value of the environment variable or the default.
func envIntOr(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

// envBoolOr returns the boolean value of the environment variable or the default.
func envBoolOr(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultVal
}

// envDurationOr returns the duration value of the environment variable or the default.
// The environment variable should be in seconds.
func envDurationOr(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultVal
}
