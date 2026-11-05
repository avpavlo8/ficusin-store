package integration

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/avpavlo8/ficusin-store/backend/internal/catalogsync"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

type cmlNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []cmlNode  `xml:",any"`
}

func node(name, text string, children ...cmlNode) cmlNode {
	return cmlNode{XMLName: xml.Name{Local: name}, Text: text, Children: children}
}
func cmlAttr(name, value string) xml.Attr { return xml.Attr{Name: xml.Name{Local: name}, Value: value} }

// Stable identifiers and the exact successfully tested prolog are intentional.
// No inventory or tax changes belong in this command.
func buildSabyCML(s catalogsync.Snapshot, price bool) ([]byte, error) {
	if e := s.Validate(); e != nil {
		return nil, e
	}
	const classifier = "69ebc10b-4a6e-50ae-bc3e-0ebd2dd41fed"
	const catalog = "47ab728e-9d1f-54f3-b4a5-223ec2a4d939"
	const pot = "a588ea1e-b55b-5ca0-9e42-f9acc06b92ae"
	const height = "f6d8e899-abf5-5bf4-8ecf-46d2a4ad63d6"
	const priceID = "049706e3-c40d-5fc1-ad72-ccac8d4c07a7"
	number := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	property := func(id, name string) cmlNode {
		return node("Свойство", "", node("Ид", id), node("Наименование", name), node("ТипЗначений", "Число"))
	}
	value := func(id string, n float64) cmlNode {
		return node("ЗначенияСвойства", "", node("Ид", id), node("Значение", number(n)))
	}
	unit := node("БазоваяЕдиница", "шт")
	unit.Attrs = []xml.Attr{cmlAttr("Код", "796"), cmlAttr("НаименованиеПолное", "Штука"), cmlAttr("МеждународноеСокращение", "PCE")}
	item := node("Товар", "", node("Ид", s.Code), node("Наименование", s.Name), unit, node("ЗначенияСвойств", "", value(pot, s.PotCM), value(height, s.HeightCM)))
	if s.Article != "" {
		item.Children = append(item.Children, node("Артикул", s.Article))
	}
	cat := node("Каталог", "", node("Ид", catalog), node("ИдКлассификатора", classifier), node("Наименование", "Каталог Фикусин"), node("Товары", "", item))
	cat.Attrs = []xml.Attr{cmlAttr("СодержитТолькоИзменения", "true")}
	root := node("КоммерческаяИнформация", "", node("Классификатор", "", node("Ид", classifier), node("Наименование", "Фикусин"), node("Свойства", "", property(pot, "Диаметр горшка"), property(height, "Высота"))), cat)
	root.Attrs = []xml.Attr{cmlAttr("ВерсияСхемы", "2.07"), cmlAttr("ДатаФормирования", time.Now().UTC().Format("2006-01-02"))}
	if price {
		amount := fmt.Sprintf("%d.%02d", s.PriceMinor/100, s.PriceMinor%100)
		offers := node("ПакетПредложений", "", node("Ид", "0e44c363-a4b6-591c-89dc-aec972386742"), node("ИдКаталога", catalog), node("ИдКлассификатора", classifier), node("ТипыЦен", "", node("ТипЦены", "", node("Ид", priceID), node("Наименование", "Основная цена"), node("Валюта", "RUB"))), node("Предложения", "", node("Предложение", "", node("Ид", s.Code), node("Наименование", s.Name), node("Цены", "", node("Цена", "", node("ИдТипаЦены", priceID), node("ЦенаЗаЕдиницу", amount), node("Валюта", "RUB"), node("Единица", "шт"), node("Коэффициент", "1"))))))
		offers.Attrs = []xml.Attr{cmlAttr("СодержитТолькоИзменения", "true")}
		root.Children = append(root.Children, offers)
	}
	b, e := xml.Marshal(root)
	if e != nil {
		return nil, e
	}
	units := utf16.Encode([]rune("<?xml version=\"1.0\" encoding=\"UTF-16\"?>\r\n" + string(b)))
	data := make([]byte, 2+2*len(units))
	data[0] = 0xff
	data[1] = 0xfe
	for i, v := range units {
		binary.LittleEndian.PutUint16(data[2+2*i:], v)
	}
	return data, nil
}

// A null response means accepted, never verified. Transport failures must not
// cause an automatic POST retry: the durable worker reconciles using GET.
func (client *SabyClient) Write(ctx context.Context, s catalogsync.Snapshot, price bool) error {
	data, e := buildSabyCML(s, price)
	if e != nil {
		return &catalogsync.RejectedError{Message: e.Error()}
	}
	payload := map[string]any{"jsonrpc": "2.0", "protocol": 3, "method": "СБИС.ИмпортНоменклатурыИзCML", "params": map[string]any{"Параметр": map[string]string{"Файл": base64.StdEncoding.EncodeToString(data)}}, "id": 1}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if e = client.authorizedJSON(ctx, http.MethodPost, client.serviceURL, payload, &response); e != nil {
		return e
	}
	if response.Error != nil {
		return &catalogsync.RejectedError{Message: "СБИС отклонил импорт: " + response.Error.Message}
	}
	if len(response.Result) == 0 {
		return errors.New("СБИС не вернул результат импорта")
	}
	return nil
}
func remoteNumber(v any) float64 {
	n, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(sabyValue(v)), ",", "."), 64)
	return n
}
func catalogueRemote(row map[string]any) *catalogsync.Remote {
	attrs, _ := row["attributes"].(map[string]any)
	return &catalogsync.Remote{ID: sabyValue(row["id"]), Code: sabyValue(row["nomNumber"]), Article: sabyValue(row["article"]), Name: sabyValue(row["name"]), PotCM: remoteNumber(attrs["Диаметр горшка"]), HeightCM: remoteNumber(attrs["Высота"]), PriceMinor: int64(math.Round(remoteNumber(row["cost"]) * 100))}
}

// Search is only discovery. Adoption requires the exact persistent code/ID.
func (client *SabyClient) Lookup(ctx context.Context, s catalogsync.Snapshot) (*catalogsync.Remote, error) {
	queries := []string{s.Name, s.LookupName}
	if fields := strings.Fields(s.Name); len(fields) > 1 {
		queries = append(queries, strings.Join(fields[:len(fields)-1], " "))
	}
	seen := map[string]bool{}
	var found *catalogsync.Remote
	var externalID string
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" || seen[query] {
			continue
		}
		seen[query] = true
		seenRows := map[string]bool{}
		for pageIndex := 0; pageIndex < 40; pageIndex++ {
			q := url.Values{"pointId": {strconv.FormatInt(client.pointID, 10)}, "searchString": {query}, "pageSize": {"25"}, "page": {strconv.Itoa(pageIndex)}}
			var page sabyCatalogPage
			if e := client.authorizedJSON(ctx, http.MethodGet, client.apiBase+"/retail/v2/nomenclature/list?"+q.Encode(), nil, &page); e != nil {
				return nil, e
			}
			rows := page.rows()
			fresh := false
			for _, row := range rows {
				if sabyBool(row["isParent"]) {
					continue
				}
				r := catalogueRemote(row)
				if seenRows[r.ID] {
					continue
				}
				seenRows[r.ID] = true
				fresh = true
				matches := r.Code == s.Code || (s.SabyID != "" && r.ID == s.SabyID)
				if matches {
					if r.Code != s.Code || (s.SabyID != "" && r.ID != s.SabyID) || (found != nil && found.ID != r.ID) {
						return nil, errors.New("СБИС вернул противоречивые идентификаторы. Проверьте связь товара")
					}
					uuid := sabyValue(row["externalId"])
					if externalID != "" && uuid != externalID {
						return nil, errors.New("СБИС вернул противоречивые UUID карточки")
					}
					externalID = uuid
					found = r
				} else if s.SabyID == "" && ((s.Article != "" && r.Article == s.Article) || strings.EqualFold(r.Name, s.Name)) {
					return nil, errors.New("В СБИС уже есть похожая карточка. Свяжите существующий товар вместо создания нового")
				}
			}
			more, known := page.hasMore()
			if len(rows) == 0 || (known && !more) || !fresh {
				break
			}
			if pageIndex == 39 {
				return nil, errors.New("Слишком много совпадений СБИС. Уточните название")
			}
		}
	}
	if found == nil {
		return nil, nil
	}
	// Search projections can retain obsolete attributes even while name and
	// price are current. Never use them as write confirmation or price preflight.
	if externalID == "" {
		return nil, errors.New("СБИС не вернул UUID для проверки карточки")
	}
	var detail map[string]any
	if e := client.authorizedJSON(ctx, http.MethodGet, client.apiBase+"/retail/nomenclature/"+url.PathEscape(externalID), nil, &detail); e != nil {
		return nil, e
	}
	current := catalogueRemote(detail)
	if current.ID != found.ID || current.Code != s.Code || sabyValue(detail["externalId"]) != externalID {
		return nil, errors.New("Идентификаторы карточки СБИС не совпали при проверке")
	}
	id, err := strconv.ParseInt(found.ID, 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("СБИС вернул неверный ID карточки")
	}
	var record map[string]any
	if err := client.rpc(ctx, "Номенклатура.Прочитать", map[string]any{"ИдО": id, "Расширение": nil}, &record); err != nil {
		return nil, err
	}
	if sabyValue(sabyRecordField(record, "@Номенклатура")) != found.ID || sabyValue(sabyRecordField(record, "НомНомер")) != s.Code || sabyValue(sabyRecordField(record, "Идентификатор")) != externalID {
		return nil, errors.New("Идентификаторы номенклатуры СБИС не совпали при прямой проверке")
	}
	attributes, _ := sabyRecordField(record, "NomenclatureExt.Attributes").(map[string]any)
	values, _ := attributes["values"].(map[string]any)
	current.PotCM = remoteNumber(values["@@_Диаметр горшка"])
	current.HeightCM = remoteNumber(values["@@_Высота"])
	current.Name = sabyValue(sabyRecordField(record, "Наименование"))
	current.Article = sabyValue(sabyRecordField(record, "NomenclatureExt.Article"))
	return current, nil
}
