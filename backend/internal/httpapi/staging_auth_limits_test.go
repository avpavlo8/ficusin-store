package httpapi

import "testing"

func TestCallLimitOnlyRelaxedInStaging(t *testing.T) {
	for _, environment := range []string{"", "production", "preview", "staging"} {
		t.Run(environment, func(t *testing.T) {
			limiter := newCallRateLimiter(environment)
			for range 5 {
				if !limiter.allow("client") {
					t.Fatal("normal login blocked")
				}
			}
			if limiter.allow("client") != (environment == "staging") {
				t.Fatal("production threshold changed or staging checks blocked")
			}
		})
	}
}
