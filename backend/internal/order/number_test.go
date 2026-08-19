package order

import "testing"

func TestFormatOrderNumber(t *testing.T) {
	tests := []struct {
		prefix   string
		sequence int
		want     string
	}{
		{prefix: "0000", sequence: 21, want: "0000-21"},
		{prefix: "0000", sequence: 22, want: "0000-22"},
		{prefix: "0001", sequence: 21, want: "0001-21"},
		{prefix: "0123", sequence: 7, want: "0123-7"},
	}
	for _, test := range tests {
		if got := formatOrderNumber(test.prefix, test.sequence); got != test.want {
			t.Fatalf("formatOrderNumber(%q, %d) = %q, want %q", test.prefix, test.sequence, got, test.want)
		}
	}
}
