from pathlib import Path

p = Path('backend/internal/integration/saby_procurement.go')
text = p.read_text()
old = '''\t\tvar page sabyCatalogPage\n\t\tif err := client.authorizedJSON(ctx, http.MethodGet, client.apiBase+"/retail/v2/nomenclature/list?"+query.Encode(), nil, &page); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tfresh := 0\n\t\tfor _, item := range page.rows() {\n\t\t\tkey := sabyValue(item["hierarchicalId"])\n\t\t\tif key == "" {\n\t\t\t\tkey = sabyValue(item["id"])\n\t\t\t}\n\t\t\tif key == "" || seenRows[key] {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tseenRows[key] = true\n\t\t\titem["sectionPath"] = append([]string(nil), sectionPath...)\n\t\t\trows = append(rows, item)\n\t\t\tfresh++\n\t\t}\n\t\tif fresh == 0 {\n\t\t\tbreak\n\t\t}\n'''
new = '''\t\tvar page sabyCatalogPage\n\t\tif err := client.authorizedJSON(ctx, http.MethodGet, client.apiBase+"/retail/v2/nomenclature/list?"+query.Encode(), nil, &page); err != nil {\n\t\t\treturn nil, err\n\t\t}\n\t\tpageRows := page.rows()\n\t\t// Saby can repeat rows on adjacent pages while still having newer cards\n\t\t// on later pages. Stop only when the API returns an actually empty page;\n\t\t// using "no fresh IDs" as EOF silently truncated recently created goods.\n\t\tif len(pageRows) == 0 {\n\t\t\tbreak\n\t\t}\n\t\tfor _, item := range pageRows {\n\t\t\tkey := sabyValue(item["hierarchicalId"])\n\t\t\tif key == "" {\n\t\t\t\tkey = sabyValue(item["id"])\n\t\t\t}\n\t\t\tif key == "" || seenRows[key] {\n\t\t\t\tcontinue\n\t\t\t}\n\t\t\tseenRows[key] = true\n\t\t\titem["sectionPath"] = append([]string(nil), sectionPath...)\n\t\t\trows = append(rows, item)\n\t\t}\n'''
if old not in text:
    raise SystemExit('pagination block not found')
p.write_text(text.replace(old, new, 1))

p = Path('backend/internal/integration/saby_procurement_test.go')
text = p.read_text()
marker = 'func TestSabyCatalogPageAcceptsObjectWrappedRows(t *testing.T) {\n'
test = r'''func TestSabyCatalogPaginationContinuesPastDuplicateOnlyPage(t *testing.T) {
	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/oauth/service/" {
			_, _ = response.Write([]byte(`{"token":"safe-token"}`))
			return
		}
		page, _ := strconv.Atoi(request.URL.Query().Get("page"))
		pages = append(pages, page)
		switch page {
		case 0:
			_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"101","name":"Антуриум Beauty Black"}]}`))
		case 1:
			_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"101","name":"Антуриум Beauty Black"}]}`))
		case 2:
			_, _ = response.Write([]byte(`{"nomenclatures":[{"id":"102","name":"Антуриум Black Love"},{"id":"103","name":"Антуриум Melodia Ibis"}]}`))
		default:
			_, _ = response.Write([]byte(`{"nomenclatures":[]}`))
		}
	}))
	defer server.Close()

	client := NewSabyClient("client", "secret", "service", 278, 6)
	client.authURL, client.apiBase, client.client = server.URL+"/oauth/service/", server.URL, server.Client()
	rows, err := client.fetchCatalogSection(context.Background(), url.Values{"pointId": {"278"}, "pageSize": {"1000"}}, "", map[string]bool{}, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d pages=%v; duplicate-only page truncated catalogue", len(rows), pages)
	}
	if sabyValue(rows[1]["id"]) != "102" || sabyValue(rows[2]["id"]) != "103" {
		t.Fatalf("rows=%+v", rows)
	}
	if fmt.Sprint(pages) != "[0 1 2 3]" {
		t.Fatalf("pages=%v", pages)
	}
}

'''
if marker not in text:
    raise SystemExit('test marker not found')
text = text.replace(marker, test + marker, 1)
text = text.replace('"net/http/httptest"\n', '"net/http/httptest"\n\t"net/url"\n', 1)
p.write_text(text)
