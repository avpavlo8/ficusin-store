package catalogeditor

import "testing"

func TestSabyMetadataGuardRequiresExactIDCodeAndKnownUUID(t *testing.T) {
	uuid := "0f56c2bd-2669-473f-9b94-875d521998a7"
	identity := SabyIdentity{ID: "1101", Code: "X269652448", Aliases: []string{uuid}}
	good := SabyMetadata{ID: identity.ID, Code: identity.Code, UUID: uuid}
	if err := ValidateSabyMetadata(identity, good); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "code", "uuid"} {
		bad := good
		switch field {
		case "id":
			bad.ID = "1102"
		case "code":
			bad.Code = "OTHER"
		case "uuid":
			bad.UUID = "00000000-0000-0000-0000-000000000000"
		}
		if ValidateSabyMetadata(identity, bad) == nil {
			t.Fatalf("mismatched %s accepted", field)
		}
	}
}
