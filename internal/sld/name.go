package sld

import "regexp"

var styleNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidStyleName reports whether a style name is safe for APIs and file lookup.
func ValidStyleName(name string) bool { return styleNamePattern.MatchString(name) }
