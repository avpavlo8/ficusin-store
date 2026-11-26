package integration

import (
	"encoding/json"
	"reflect"
	"testing"

	ce "github.com/avpavlo8/ficusin-store/backend/internal/catalogeditor"
)

func TestProcurementMeasurementsUsePhysicalMarketplaceAttributes(t *testing.T) {
	request := ce.Request{
		Action: "updating", ProcurementMeasurements: true,
		SiteAttributes: map[string]any{"pot_diameter_cm": 14.5, "height_cm": 32.0},
		Schema: []ce.Attribute{
			{ID: 1, Name: "Диаметр горшка, см", Type: "Decimal"},
			{ID: 2, Name: "Высота растения, мм", Type: "Integer"},
			{ID: 3, Name: "Высота упаковки, см", Type: "Integer"},
		},
		Channel: ce.Channel{Attributes: map[string]any{"3": 51.0}},
	}
	attrs, err := catalogAttributes("ozon", request)
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 3 || !hasProcurementMarketplaceMeasurement(request, "ozon") {
		t.Fatalf("expected both physical fields and unchanged packaging, got %#v", attrs)
	}
	want := []string{"14.5", "320", "51"}
	for i, attr := range attrs {
		got := attr["values"].([]map[string]any)[0]["value"]
		if got != want[i] {
			t.Fatalf("field %d: want %q got %v", i, want[i], got)
		}
	}
}

func TestFlowwowProcurementMeasurementsUseDeclaredCentimeters(t *testing.T) {
	var field FlowwowField
	if err := json.Unmarshal([]byte(`{"field":"height","name":"Высота","type":"measure","measure":[{"id":2,"name":"мм"},{"id":9,"name":"см"}]}`), &field); err != nil {
		t.Fatal(err)
	}
	code, value := flowwowPlantMeasurement(field, map[string]any{"height_cm": 37.5})
	if code != "height_cm" || !reflect.DeepEqual(value, map[string]any{"val": 37.5, "measure": int64(9)}) {
		t.Fatalf("expected named centimeter unit, got %q %#v", code, value)
	}
	field.Name = "Высота упаковки"
	if code, _ := flowwowPlantMeasurement(field, map[string]any{"height_cm": 37.5}); code != "" {
		t.Fatalf("physical height must not override packaging height: %q", code)
	}
}

func TestSabyProcurementMeasurementUpdatesOnlyDiameterInName(t *testing.T) {
	fixture := newSabyEditorNativeFixture(t)
	fixture.lostUpdate = true
	preserved := map[string][]byte{}
	for _, field := range []string{"Number", "Description", "Category", "Folder", "CodeList", "ImageData", "PriceData"} {
		preserved[field], _ = json.Marshal(sabyRecordField(fixture.card, field))
	}
	request := ce.Request{
		ProcurementMeasurements: true, Action: "update", ExistingID: "1101", SabyID: "1101",
		SabyUUID: "0f56c2bd-2669-473f-9b94-875d521998a7", Name: "Адениум D14.5",
	}
	result := driveSabyEditorFixture(t, fixture.publisher(), request, ce.Progress{})
	if result.Status != "synced" || fixture.creates != 0 || fixture.updates != 1 || fixture.uploads != 0 || result.Progress.Ambiguous {
		t.Fatalf("unexpected Saby measurement publication outcome: %+v", result)
	}
	for field, before := range preserved {
		after, _ := json.Marshal(sabyRecordField(fixture.card, field))
		if string(before) != string(after) {
			t.Errorf("unrelated Saby field %q changed", field)
		}
	}
	if got := sabyRecordField(fixture.card, "Name"); got != request.Name {
		t.Fatalf("expected updated name %q; got %v", request.Name, got)
	}
}
