package operations

import "testing"

func TestStatusFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		checks []Check
		want   string
	}{
		{name: "healthy", want: "ok"},
		{name: "warning", checks: []Check{{Severity: "warning"}}, want: "degraded"},
		{name: "critical", checks: []Check{{Severity: "warning"}, {Severity: "critical"}}, want: "unavailable"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := statusFor(test.checks); got != test.want {
				t.Fatalf("statusFor() = %q, want %q", got, test.want)
			}
		})
	}
}
