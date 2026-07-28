package config

import "testing"

func TestDatabaseTLSVerificationEnabled(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"":        true,
		"true":    true,
		"FALSE":   false,
		" false ": false,
		"0":       false,
	}

	for value, expected := range tests {
		if actual := databaseTLSVerificationEnabled(value); actual != expected {
			t.Errorf("databaseTLSVerificationEnabled(%q) = %v, want %v", value, actual, expected)
		}
	}
}

func TestNormalizedPEM(t *testing.T) {
	t.Parallel()

	input := `"-----BEGIN CERTIFICATE-----\nvalue\n-----END CERTIFICATE-----"`
	expected := "-----BEGIN CERTIFICATE-----\nvalue\n-----END CERTIFICATE-----"
	if actual := normalizedPEM(input); actual != expected {
		t.Fatalf("normalizedPEM() = %q, want %q", actual, expected)
	}
}

func TestBoundedInteger(t *testing.T) {
	t.Parallel()

	if actual := boundedInteger("120", 30, 1, 90); actual != 90 {
		t.Fatalf("boundedInteger() = %d, want 90", actual)
	}
	if actual := boundedInteger("invalid", 30, 1, 90); actual != 30 {
		t.Fatalf("boundedInteger() = %d, want 30", actual)
	}
}
