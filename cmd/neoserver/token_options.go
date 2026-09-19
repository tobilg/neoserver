package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func tokenDuration(value string) (time.Duration, error) {
	var duration time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		days, parseErr := strconv.ParseUint(strings.TrimSuffix(value, "d"), 10, 64)
		if parseErr != nil || days == 0 || days > uint64(math.MaxInt64/int64(24*time.Hour)) {
			return 0, fmt.Errorf("invalid expiry; use a positive duration such as 24h or 7d")
		}
		duration = time.Duration(days) * 24 * time.Hour
	} else {
		duration, err = time.ParseDuration(value)
	}
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid expiry; use a positive duration such as 24h or 7d")
	}
	return duration, nil
}

func validTokenRole(role string) bool {
	return role == "super_admin" || role == "admin" || role == "editor" || role == "viewer"
}
