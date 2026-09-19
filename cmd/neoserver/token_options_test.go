package main

import "testing"

func TestTokenOptions(t *testing.T) {
	for _, value := range []string{"", "0", "0d", "-1h", "-1d", "7days", "9999999999999999999d", "wat"} {
		if _, err := tokenDuration(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"24h", "7d", "30m"} {
		if _, err := tokenDuration(value); err != nil {
			t.Error(err)
		}
	}
	if validTokenRole("owner") {
		t.Fatal("accepted unknown role")
	}
}
