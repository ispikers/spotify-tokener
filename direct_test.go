package main

import (
	"testing"
	"time"
)

func TestTOTPMatchesRFC6238(t *testing.T) {
	key := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59:         "287082",
		1111111109: "081804",
		1234567890: "005924",
		2000000000: "279037",
	} {
		if got := totp(key, time.Unix(unix, 0)); got != want {
			t.Errorf("totp at %d = %s, want %s", unix, got, want)
		}
	}
}
