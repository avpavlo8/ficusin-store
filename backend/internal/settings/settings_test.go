package settings

import "testing"

// A setting nobody has touched must still mean something sensible: the shop
// starts before anyone opens the panel.
func TestUntouchedSettingsFallBackToDefaults(t *testing.T) {
	service := &Service{values: map[string]string{}}

	if !service.Enabled(PaymentsEnabled) {
		t.Fatal("оплата должна быть включена по умолчанию")
	}
	if service.Enabled(CDEKOrdersEnabled) {
		t.Fatal("создание заказов в СДЭК по умолчанию выключено")
	}
	if hours := service.Number(AutoCancelHours); hours != 24 {
		t.Fatalf("срок автоотмены = %d, ожидали 24", hours)
	}
}

// Only "0" switches something off. A stray value must not silently take
// payments offline — failing towards a working shop is the safer direction.
func TestOnlyZeroSwitchesSomethingOff(t *testing.T) {
	for value, wanted := range map[string]bool{
		"0":     false,
		" 0 ":   false,
		"1":     true,
		"":      true,
		"true":  true,
		"мусор": true,
	} {
		service := &Service{values: map[string]string{PaymentsEnabled: value}}
		if service.Enabled(PaymentsEnabled) != wanted {
			t.Fatalf("значение %q дало %v, ожидали %v", value, !wanted, wanted)
		}
	}
}

// The panel draws whatever is in Definitions, so every one of them needs a
// default — otherwise a switch would appear with no meaning behind it.
func TestEveryDefinitionHasADefault(t *testing.T) {
	for _, definition := range Definitions {
		if _, found := defaults[definition.Key]; !found {
			t.Fatalf("у настройки %s нет значения по умолчанию", definition.Key)
		}
	}
}
