package procurement

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// BuildSabyPriceXLSX creates the two-column workbook from Saby's official
// "Загрузка цен по коду" sample. Saby maps Код to its catalogue card and Цена
// to the manually entered price; no calculated markup column is used.
func BuildSabyPriceXLSX(lines []OrderLine) ([]byte, int, error) {
	type priceRow struct {
		code  string
		price int64
	}
	byProduct := make(map[string]priceRow)
	for _, line := range lines {
		if line.MatchStatus != "confirmed" || line.ProposedRetailRUB == nil || *line.ProposedRetailRUB <= 0 {
			continue
		}
		code := strings.TrimSpace(line.SabyCode)
		if code == "" {
			code = strings.TrimSpace(line.SabyID)
		}
		if code == "" {
			continue
		}
		key := strings.TrimSpace(line.SabyID)
		if key == "" {
			key = code
		}
		candidate := priceRow{code: code, price: *line.ProposedRetailRUB}
		if current, exists := byProduct[key]; !exists || candidate.price > current.price {
			byProduct[key] = candidate
		}
	}
	rows := make([]priceRow, 0, len(byProduct))
	for _, row := range byProduct {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].code < rows[j].code })
	if len(rows) == 0 {
		return nil, 0, errors.New("в закупке нет рассчитанных цен для выгрузки Saby")
	}

	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="1" width="22" customWidth="1"/><col min="2" max="2" width="16" customWidth="1"/></cols><sheetData>`)
	sheet.WriteString(`<row r="1"><c r="A1" t="inlineStr" s="1"><is><t>Код</t></is></c><c r="B1" t="inlineStr" s="1"><is><t>Цена</t></is></c></row>`)
	for index, row := range rows {
		r := index + 2
		sheet.WriteString(`<row r="` + strconv.Itoa(r) + `"><c r="A` + strconv.Itoa(r) + `" t="inlineStr"><is><t>` + xmlText(row.code) + `</t></is></c><c r="B` + strconv.Itoa(r) + `"><v>` + strconv.FormatInt(row.price, 10) + `</v></c></row>`)
	}
	sheet.WriteString(`</sheetData><autoFilter ref="A1:B` + strconv.Itoa(len(rows)+1) + `"/></worksheet>`)

	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Загрузка цен по коду" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`,
		"xl/styles.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`,
		"xl/worksheets/sheet1.xml": sheet.String(),
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		entry, err := archive.Create(path)
		if err != nil {
			return nil, 0, fmt.Errorf("создать XLSX: %w", err)
		}
		if _, err := entry.Write([]byte(files[path])); err != nil {
			return nil, 0, fmt.Errorf("записать XLSX: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, 0, fmt.Errorf("закрыть XLSX: %w", err)
	}
	return output.Bytes(), len(rows), nil
}

func xmlText(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	return strings.ReplaceAll(value, "'", "&apos;")
}
