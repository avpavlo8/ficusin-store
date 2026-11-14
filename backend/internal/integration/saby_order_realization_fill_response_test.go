package integration

import (
	"context"
	"encoding/json"
	"testing"
)

func actualSaveFixture(t *testing.T) map[string]any {
	t.Helper()
	value, err := sabyFillDecode([]byte(actualSabySave43560173398))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSabyOrderRealizationActualSaveCompactEmptyUpdateAcknowledgesExactID(t *testing.T) {
	actual := actualSaveFixture(t)
	updated := sabyRecordField(actual, "upd_rs")
	if _, err := sabyFillRecordset(updated); err == nil || err.Error() != "Нет полной схемы строк" {
		t.Fatalf("fixture did not reproduce original rejection: %v", err)
	}
	runActualSaveFixture(t, nil, true)
}

func runActualSaveFixture(t *testing.T, edit func(map[string]any), wantSaved bool) {
	t.Helper()
	actual := actualSaveFixture(t)
	if edit != nil {
		edit(actual)
	}
	client, h := newFillHarness(t)
	expected := fillRequest()
	expected.Identity.ID = 42807
	expected.RegulationID = 122498
	expected.Next = SabyOrderRealizationFillLine{NomenclatureID: 3899, Quantity: "1", UnitPrice: "1.01"}
	expected.Lines = []SabyOrderRealizationFillLine{expected.Next}
	setSabyRecordField(h.header, "@Документ", int64(42807))
	setSabyRecordField(h.header, "Регламент", int64(122498))
	// Preparing a native template still exercises the exact normal preflight and
	// saved-row hash. Its input row comes from the untouched captured fixture.
	rows, err := sabyFillRecordset(sabyRecordField(actualSaveFixture(t), "new_rs"))
	if err != nil || len(rows) != 1 {
		t.Fatal("fixture rows", err)
	}
	h.template = rows[0]
	setSabyRecordField(h.template, "@НоменклатураДокумента", nil)
	setSabyRecordField(h.template, "Закрыто", nil)
	h.editReply = func(method string, reply map[string]any) {
		if method == "РеалИсх.CustomDocNomUpdate" {
			for k := range reply {
				delete(reply, k)
			}
			for k, v := range actual {
				reply[k] = v
			}
		}
	}
	prepared, err := client.PrepareOrderRealizationRow(context.Background(), expected)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := client.SavePreparedOrderRealizationRow(context.Background(), expected, *prepared.Prepared)
	if wantSaved {
		if err != nil || saved.Outcome != "saved" || saved.RowID != 175676 || saved.HTTPStatus != 200 {
			t.Fatalf("actual valid acknowledgement rejected: %v outcome=%s row=%d", err, saved.Outcome, saved.RowID)
		}
	} else if err == nil || saved.Outcome != "unknown" || saved.RowID != 0 {
		t.Fatalf("unproven ID accepted: %v outcome=%s row=%d", err, saved.Outcome, saved.RowID)
	}
	if len(h.writes) != 2 || h.writes[0] != "РеалИсх.DocNomCreate" || h.writes[1] != "РеалИсх.CustomDocNomUpdate" {
		t.Fatal("unexpected save replay", h.writes)
	}
	var receipt map[string]any
	if json.Unmarshal(saved.Receipt, &receipt) != nil {
		t.Fatal("lost raw save receipt")
	}
}

func TestSabyOrderRealizationCompactSaveStillRejectsUnknownFormatsAndForeignRows(t *testing.T) {
	cases := map[string]func(map[string]any){
		"unknown format reference": func(a map[string]any) { sabyRecordField(a, "upd_rs").(map[string]any)["f"] = json.Number("2") },
		"missing format reference": func(a map[string]any) { delete(sabyRecordField(a, "upd_rs").(map[string]any), "f") },
		"nonempty compact updates": func(a map[string]any) {
			sabyRecordField(a, "upd_rs").(map[string]any)["d"] = []any{[]any{json.Number("175676")}}
		},
		"missing sibling schema": func(a map[string]any) { delete(sabyRecordField(a, "new_rs").(map[string]any), "s") },
		"wrong acknowledged row": func(a map[string]any) { setSabyRecordField(a, "result", json.Number("175677")) },
		"wrong document": func(a map[string]any) {
			set := sabyRecordField(a, "new_rs").(map[string]any)
			rows, _ := sabyFillRecordset(set)
			setSabyRecordField(rows[0], "СкладскойДокумент", json.Number("42808"))
		},
		"wrong price": func(a map[string]any) {
			set := sabyRecordField(a, "new_rs").(map[string]any)
			rows, _ := sabyFillRecordset(set)
			setSabyRecordField(rows[0], "Цена", json.Number("1.02"))
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) { runActualSaveFixture(t, edit, false) })
	}
}

// Safe projection of the real protocol-7 save acknowledgement from run
// 43560173398. Only unused business columns and totals were removed; all
// identity, price, state schemas, values and the compact format reference remain.
const actualSabySave43560173398 = `{"f":0,"d":[175676,{},"both",{"f":1,"d":[[175676,1.0,1,1.01,[null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null],false,1.01,3899,284,42807]],"s":[{"n":"@НоменклатураДокумента","t":"Число целое"},{"n":"Количество","t":"Число вещественное"},{"n":"Тип","t":{"n":"Перечисляемое","s":{"null":null,"-1":"Резерв","0":"Приход","1":"Расход","2":"ФактическийОстаток","3":"ОстатокПоДокументам","4":"ПрестоЗаказВозвратДенег","5":"Позиция с детализированными характеристиками","6":"Запланировано","7":"СоставНаименования","8":"СебестоимостьСоставляющей","9":"НаименованиеАВ","10":"СписаниеВРасход","11":"СписаниеСПартии","12":"РазницаКоррФактуры","13":"Валюта","14":"Упаковка","15":"Наименования магазина подарков","16":"Приход топлива","17":"СметаУчётОстатка","20":"Фактический остаток по партии","30":"Учетный остаток по партии","40":"Размещение","41":"Отбор","42":"Перемещение","80":"Приход прослеживаемости","81":"Расход прослеживаемости"},"sl":{"null":null,"-1":"Резерв","0":"Приход","1":"Расход","2":"ФактическийОстаток","3":"ОстатокПоДокументам","4":"ПрестоЗаказВозвратДенег","5":"Позиция с детализированными характеристиками","6":"Запланировано","7":"СоставНаименования","8":"СебестоимостьСоставляющей","9":"НаименованиеАВ","10":"СписаниеВРасход","11":"СписаниеСПартии","12":"РазницаКоррФактуры","13":"Валюта","14":"Упаковка","15":"Наименования магазина подарков","16":"Приход топлива","17":"СметаУчётОстатка","20":"Фактический остаток по партии","30":"Учетный остаток по партии","40":"Размещение","41":"Отбор","42":"Перемещение","80":"Приход прослеживаемости","81":"Расход прослеживаемости"}}},{"n":"СуммаЦен","t":{"n":"Деньги","p":2}},{"n":"Флаги","t":{"n":"Флаги","s":{"0":"Удален","1":"ВнутреннееДвижение","2":"Непросчитано","3":"ФиксироватьОстатки","4":"ОтгрузкаПодарок","5":"Не используется 1","6":"Вручную","7":"Изменен","8":"ТипПроведенияАИ","9":"НаличиеРезерва","10":"ReturnIn","11":"ReturnOut","12":"CapitalDoc","13":"Наряд","14":"Waybill","15":"Корректировка","16":"Party Formation","17":"Расходы в ТМЦ","18":"ФиксацияСебестоимости","19":"НачальныеОстатки","20":"RecalcRemains","21":"ВнутреннийОборот","22":"Аналог","23":"ИзмененАналог","24":"Оплачено","25":"Отгружено","26":"БыстрыйРасчетПрихода","27":"ПриходТоплива","28":"ПриходТопливаОплатаПоКарте","29":"СложнаяИерархия","30":"ПриходТопливаПоМашине","31":"Размещение"},"sl":{"0":"Удален","1":"ВнутреннееДвижение","2":"Непросчитано","3":"ФиксироватьОстатки","4":"ОтгрузкаПодарок","5":"Не используется 1","6":"Вручную","7":"Изменен","8":"ТипПроведенияАИ","9":"НаличиеРезерва","10":"ReturnIn","11":"ReturnOut","12":"CapitalDoc","13":"Наряд","14":"Waybill","15":"Корректировка","16":"Party Formation","17":"Расходы в ТМЦ","18":"ФиксацияСебестоимости","19":"НачальныеОстатки","20":"RecalcRemains","21":"ВнутреннийОборот","22":"Аналог","23":"ИзмененАналог","24":"Оплачено","25":"Отгружено","26":"БыстрыйРасчетПрихода","27":"ПриходТоплива","28":"ПриходТопливаОплатаПоКарте","29":"СложнаяИерархия","30":"ПриходТопливаПоМашине","31":"Размещение"}}},{"n":"Закрыто","t":"Логическое"},{"n":"Цена","t":{"n":"Деньги","p":2}},{"n":"Номенклатура","t":"Число целое"},{"n":"Склад","t":"Число целое"},{"n":"СкладскойДокумент","t":"Число целое"}],"tp":"НоменклатураДокумента","_type":"recordset"},{"f":1,"d":[],"_type":"recordset"},null,null,null,{"f":2,"d":[],"s":[],"_type":"record"}],"s":[{"n":"result","t":"Число целое"},{"n":"details","t":"JSON-объект"},{"n":"nav_res","t":"Строка"},{"n":"new_rs","t":"Выборка"},{"n":"upd_rs","t":"Выборка"},{"n":"del_rs","t":"Выборка"},{"n":"missed_rs","t":"Выборка"},{"n":"data","t":"Запись"},{"n":"total_rec","t":"Запись"}],"_type":"record"}`
