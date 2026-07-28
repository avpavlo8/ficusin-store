package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	t.Parallel()

	encoded, err := HashPassword("Пароль12345")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("Пароль12345", encoded) {
		t.Fatal("generated password hash was not verified")
	}
	if VerifyPassword("Неверный12345", encoded) {
		t.Fatal("wrong password was accepted")
	}
}

func TestNodePasswordCompatibility(t *testing.T) {
	t.Parallel()

	const encoded = "scrypt$16384$8$1$AAECAwQFBgcICQoLDA0ODw$rCr-fTsG0fQy1vPHYftSgPTI8atgfFcCf-T471-katphlGsKGANWy-fQ9MFAXCW8aT2eNg0JPYnMwnA_lGWxEw"
	if !VerifyPassword("Ficusin123", encoded) {
		t.Fatal("Node.js-compatible hash was not verified")
	}
}

func TestPasswordRules(t *testing.T) {
	t.Parallel()

	if !PasswordIsAcceptable("Растение123") {
		t.Fatal("valid password was rejected")
	}
	for _, password := range []string{"short1", "толькобуквы", "1234567890"} {
		if PasswordIsAcceptable(password) {
			t.Fatalf("invalid password %q was accepted", password)
		}
	}
}
