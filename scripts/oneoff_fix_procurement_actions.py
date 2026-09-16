from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one occurrence, got {count}: {old[:160]!r}")
    p.write_text(text.replace(old, new, 1))


# Saby XLSX: only products whose final/highest proposed retail price is marked
# as a meaningful change by the same threshold used in the procurement UI.
replace_once(
    "backend/internal/procurement/saby_price_xlsx.go",
    "\ttype priceRow struct {\n\t\tcode  string\n\t\tprice int64\n\t}",
    "\ttype priceRow struct {\n\t\tcode   string\n\t\tprice  int64\n\t\tneeded bool\n\t}",
)
replace_once(
    "backend/internal/procurement/saby_price_xlsx.go",
    "\t\tcandidate := priceRow{code: code, price: *line.ProposedRetailRUB}\n\t\tif current, exists := byProduct[key]; !exists || candidate.price > current.price {\n\t\t\tbyProduct[key] = candidate\n\t\t}\n\t}\n\trows := make([]priceRow, 0, len(byProduct))\n\tfor _, row := range byProduct {\n\t\trows = append(rows, row)\n\t}",
    "\t\tcandidate := priceRow{code: code, price: *line.ProposedRetailRUB, needed: line.PriceChangeNeeded}\n\t\tif current, exists := byProduct[key]; !exists || candidate.price > current.price {\n\t\t\tbyProduct[key] = candidate\n\t\t}\n\t}\n\trows := make([]priceRow, 0, len(byProduct))\n\tfor _, row := range byProduct {\n\t\tif row.needed {\n\t\t\trows = append(rows, row)\n\t\t}\n\t}",
)

# Site follows the same red-row/Saby-retail threshold. WB and Ozon intentionally
# continue receiving every connected calculated item.
replace_once(
    "backend/internal/procurement/postgres_actions.go",
    "\t\t\tWHERE channel.name = ANY($3::TEXT[])\n\t\t`, batchID, orderID, channels)",
    "\t\t\tWHERE channel.name = ANY($3::TEXT[])\n\t\t\t\tAND (channel.name IN ('wb','ozon')\n\t\t\t\t\tOR n.price_minor <= 0\n\t\t\t\t\tOR ABS(p.retail::NUMERIC - n.price_minor::NUMERIC / 100)\n\t\t\t\t\t\t> (n.price_minor::NUMERIC / 100) *\n\t\t\t\t\t\t\t(SELECT price_change_threshold FROM procurement_pricing_settings WHERE id=1))\n\t\t`, batchID, orderID, channels)",
)
replace_once(
    "backend/internal/procurement/postgres_actions.go",
    "\t\t\t\t\tGROUP BY l.saby_id, n.code, n.name, n.price_minor\n\t\t\t\t)",
    "\t\t\t\t\tGROUP BY l.saby_id, n.code, n.name, n.price_minor\n\t\t\t\t\tHAVING n.price_minor <= 0\n\t\t\t\t\t\tOR ABS(MAX(l.proposed_retail_rub)::NUMERIC - n.price_minor::NUMERIC / 100)\n\t\t\t\t\t\t\t> (n.price_minor::NUMERIC / 100) *\n\t\t\t\t\t\t\t\t(SELECT price_change_threshold FROM procurement_pricing_settings WHERE id=1)\n\t\t\t\t)",
)

# Refresh Saby immediately before preparing a receipt so the preview snapshots
# actual current balances rather than a stale local zero.
replace_once(
    "backend/internal/procurement/service.go",
    "\t}\n\treturn service.store.PrepareBatch(ctx, actor, orderID, kind, selected)\n}\n\nfunc channelDisplayName",
    "\t}\n\tif kind == \"receipt\" && service.executor != nil && service.executor.Configured(\"saby\") {\n\t\tif refresher, ok := service.executor.(SabyCatalogRefresher); ok {\n\t\t\tif _, err := refresher.RefreshSabyCatalog(ctx); err != nil {\n\t\t\t\treturn ActionBatch{}, &UserFacingError{Message: \"Не удалось обновить актуальные остатки СБИС перед поступлением. Повторите подготовку поступления позже.\"}\n\t\t\t}\n\t\t}\n\t}\n\treturn service.store.PrepareBatch(ctx, actor, orderID, kind, selected)\n}\n\nfunc channelDisplayName",
)

# Never append receipt rows to an already-created Saby document. A temporarily
# unreadable read-back used to be interpreted as an empty document and the same
# 36 rows were appended again on the retry.
replace_once(
    "backend/internal/integration/saby_procurement.go",
    "\tinternalID, _ := strconv.ParseInt(strings.TrimSpace(item.ExternalOperationID), 10, 64)\n\tlink := safeSabyURL(item.ExternalURL)",
    "\tinternalID, _ := strconv.ParseInt(strings.TrimSpace(item.ExternalOperationID), 10, 64)\n\tcreatedNow := internalID <= 0\n\tlink := safeSabyURL(item.ExternalURL)",
)
replace_once(
    "backend/internal/integration/saby_procurement.go",
    "\tvar document map[string]any\n\tsavedLineCount := 0\n\tvar err error\n\tif internalID > 0 {\n\t\tdocument, savedLineCount, err = client.readReceipt(ctx, internalID)",
    "\tvar document map[string]any\n\tvar err error\n\tif internalID > 0 {\n\t\tdocument, _, err = client.readReceipt(ctx, internalID)",
)
replace_once(
    "backend/internal/integration/saby_procurement.go",
    "\tif savedLineCount == 0 {",
    "\tif createdNow {",
)

# Saby internal recordsets sometimes prefix or suffix these schema names.
replace_once(
    "backend/internal/integration/saby_procurement.go",
    "func sabyFieldIndex(fields []any, name string) int {\n\tfor index, raw := range fields {\n\t\tfield, _ := raw.(map[string]any)\n\t\tif field[\"n\"] == name {\n\t\t\treturn index\n\t\t}\n\t}\n\treturn -1\n}",
    "func sabyFieldIndex(fields []any, name string) int {\n\twanted := normalizeSabyFieldName(name)\n\tfor index, raw := range fields {\n\t\tfield, _ := raw.(map[string]any)\n\t\tif normalizeSabyFieldName(field[\"n\"]) == wanted {\n\t\t\treturn index\n\t\t}\n\t}\n\tfor index, raw := range fields {\n\t\tfield, _ := raw.(map[string]any)\n\t\tactual := normalizeSabyFieldName(field[\"n\"])\n\t\tif wanted == \"номенклатура\" && strings.Contains(actual, \"номенклатур\") {\n\t\t\treturn index\n\t\t}\n\t\tif wanted == \"количество\" && strings.Contains(actual, \"количеств\") && !strings.Contains(actual, \"мест\") {\n\t\t\treturn index\n\t\t}\n\t}\n\treturn -1\n}\n\nfunc normalizeSabyFieldName(value any) string {\n\tname := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))\n\tname = strings.TrimLeft(name, \"@\")\n\treturn strings.NewReplacer(\" \", \"\", \"_\", \"\", \"-\", \"\").Replace(name)\n}",
)

# Make the asynchronous WB state explicit after an upload id has been issued.
replace_once(
    "frontend/src/AdminProcurementDialogs.tsx",
    "  const label = item.channel === \"saby_receipt\" && item.status === \"draft\" ? \"Подготовлено\" : item.channel === \"saby_receipt\" && item.externalUrl && [\"queued\", \"processing\"].includes(item.status) ? \"Черновик создан · ждёт проведения\" : item.channel === \"saby_receipt\" && item.status === \"completed\" ? \"Проведение подтверждено\" : (({ draft: \"Черновик\", queued: \"В очереди\", processing: \"Отправляется\", completed: \"Выполнено\", failed: \"Ошибка\", skipped: \"Пропущено\", not_configured: \"API не подключён\" } as Record<string, string>)[item.status] || item.status);",
    "  const label = item.channel === \"saby_receipt\" && item.status === \"draft\" ? \"Подготовлено\" : item.channel === \"saby_receipt\" && item.externalUrl && [\"queued\", \"processing\"].includes(item.status) ? \"Черновик создан · ждёт проведения\" : item.channel === \"saby_receipt\" && item.status === \"completed\" ? \"Проведение подтверждено\" : item.channel === \"wb\" && item.externalOperationId && [\"queued\", \"processing\"].includes(item.status) ? \"Wildberries обрабатывает загрузку\" : (({ draft: \"Черновик\", queued: \"В очереди\", processing: \"Отправляется\", completed: \"Выполнено\", failed: \"Ошибка\", skipped: \"Пропущено\", not_configured: \"API не подключён\" } as Record<string, string>)[item.status] || item.status);",
)

# XLSX regression test.
p = Path("backend/internal/procurement/saby_price_xlsx_test.go")
text = p.read_text()
text = text.replace(
    "\tfirst, second, ignored := int64(2650), int64(2790), int64(9999)\n\tcontent, count, err := BuildSabyPriceXLSX([]OrderLine{\n\t\t{SabyID: \"42\", SabyCode: \"X42\", MatchStatus: \"confirmed\", ProposedRetailRUB: &first},\n\t\t{SabyID: \"42\", SabyCode: \"X42\", MatchStatus: \"confirmed\", ProposedRetailRUB: &second},\n\t\t{SabyID: \"99\", SabyCode: \"X99\", MatchStatus: \"ignored\", ProposedRetailRUB: &ignored},\n\t})",
    "\tfirst, second, ignored, unchanged := int64(2650), int64(2790), int64(9999), int64(2750)\n\tcontent, count, err := BuildSabyPriceXLSX([]OrderLine{\n\t\t{SabyID: \"42\", SabyCode: \"X42\", MatchStatus: \"confirmed\", ProposedRetailRUB: &first, PriceChangeNeeded: true},\n\t\t{SabyID: \"42\", SabyCode: \"X42\", MatchStatus: \"confirmed\", ProposedRetailRUB: &second, PriceChangeNeeded: true},\n\t\t{SabyID: \"77\", SabyCode: \"X77\", MatchStatus: \"confirmed\", ProposedRetailRUB: &unchanged, PriceChangeNeeded: false},\n\t\t{SabyID: \"99\", SabyCode: \"X99\", MatchStatus: \"ignored\", ProposedRetailRUB: &ignored, PriceChangeNeeded: true},\n\t})",
)
text = text.replace(
    "\tif strings.Contains(sheet, \"X99\") { t.Fatalf(\"ignored product leaked into sheet: %s\", sheet) }",
    "\tif strings.Contains(sheet, \"X99\") { t.Fatalf(\"ignored product leaked into sheet: %s\", sheet) }\n\tif strings.Contains(sheet, \"X77\") { t.Fatalf(\"unchanged price leaked into sheet: %s\", sheet) }",
)
p.write_text(text)

# Receipt refresh regression test.
p = Path("backend/internal/procurement/service_test.go")
text = p.read_text()
anchor = "type probeExecutorStub struct{ probeCalls int }\n"
insert = """type sabyRefreshExecutorStub struct{ refreshCalls int }\n\nfunc (stub *sabyRefreshExecutorStub) Configured(channel string) bool { return channel == \"saby\" }\nfunc (*sabyRefreshExecutorStub) Execute(context.Context, ActionItem) (ActionExecution, error) {\n\treturn ActionExecution{}, nil\n}\nfunc (stub *sabyRefreshExecutorStub) RefreshSabyCatalog(context.Context) (ChannelLinkResult, error) {\n\tstub.refreshCalls++\n\treturn ChannelLinkResult{Channel: \"saby\", Fetched: 10, Linked: 10}, nil\n}\n\n"""
if insert not in text:
    text = text.replace(anchor, insert + anchor)
test_anchor = "func TestWildberriesConnectionCheckReadsMirrorWithoutCallingAPI(t *testing.T) {"
new_test = """func TestPrepareReceiptRefreshesSabyBalancesBeforeSnapshot(t *testing.T) {\n\tt.Parallel()\n\tstore := &storeStub{}\n\texecutor := &sabyRefreshExecutorStub{}\n\tservice := NewServiceWithExecutor(store, executor)\n\tbatch, err := service.PrepareBatch(context.Background(), Actor{}, 18, \"receipt\", nil)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n\tif batch.Kind != \"receipt\" || executor.refreshCalls != 1 {\n\t\tt.Fatalf(\"batch=%+v refreshCalls=%d\", batch, executor.refreshCalls)\n\t}\n}\n\n"""
if new_test not in text:
    text = text.replace(test_anchor, new_test + test_anchor)
p.write_text(text)

# Replace unsafe retry expectation with idempotency regression.
p = Path("backend/internal/integration/saby_procurement_test.go")
text = p.read_text()
start = text.index("func TestSabyReceiptRetryUsesStableExternalID")
end = text.index("\nfunc TestSabyReceiptRetryReplacesUnaddressablePublicHeader", start)
replacement = '''func TestSabyReceiptRetryNeverAppendsRowsToExistingDocument(t *testing.T) {
\tconst receiptGUID = "22222222-2222-4222-8222-222222222222"
\tvar creates, adds int
\tserver := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
\t\tresponse.Header().Set("Content-Type", "application/json")
\t\tif request.URL.Path == "/oauth/service/" {
\t\t\t_, _ = response.Write([]byte(`{"token":"safe-token"}`))
\t\t\treturn
\t\t}
\t\tvar rpc struct { Method string `json:"method"` }
\t\tif err := json.NewDecoder(request.Body).Decode(&rpc); err != nil { t.Fatal(err) }
\t\tswitch rpc.Method {
\t\tcase "РеалВх.Создать":
\t\t\tcreates++
\t\t\t_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"record","d":[8,%q],"s":[{"n":"@Документ"},{"n":"ИдентификаторДокумента"}]}}`, receiptGUID)
\t\tcase "РеалВх.NomCreateWithSaveBatch":
\t\t\tadds++
\t\t\t_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"temporary add error"}}`))
\t\tcase "ДокОтгрВх.Прочитать":
\t\t\t_, _ = fmt.Fprintf(response, `{"jsonrpc":"2.0","id":1,"result":{"_type":"record","d":[8,%q,false,{"_type":"recordset","d":[],"s":[{"n":"@Номенклатура"},{"n":"КоличествоОсн"}]}],"s":[{"n":"@Документ"},{"n":"ИдентификаторДокумента"},{"n":"Проведен"},{"n":"Строки"}]}}`, receiptGUID)
\t\tdefault:
\t\t\tt.Fatalf("unexpected method: %s", rpc.Method)
\t\t}
\t}))
\tdefer server.Close()
\tclient := NewSabyClient("client", "secret", "service", 278, 6)
\tclient.authURL, client.serviceURL, client.client = server.URL+"/oauth/service/", server.URL+"/service/?srv=1", server.Client()
\tpayload := json.RawMessage(`{"orderId":323,"lines":[{"sabyId":"42","name":"Орхидея D12","quantity":2}]}`)
\tfirst, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_receipt", Payload: payload})
\tif err == nil || first.ExternalOperationID != "8" { t.Fatalf("first=%+v err=%v", first, err) }
\tsecond, err := client.CreateDraft(context.Background(), procurement.ActionItem{Channel: "saby_receipt", Payload: payload, ExternalOperationID: first.ExternalOperationID, ExternalURL: first.ExternalURL})
\tif err == nil || !strings.Contains(err.Error(), "0 товарных строк из 1") { t.Fatalf("second=%+v err=%v", second, err) }
\tif creates != 1 || adds != 1 { t.Fatalf("creates=%d adds=%d; retry duplicated Saby rows", creates, adds) }
}
'''
text = text[:start] + replacement + text[end:]
alias_test = '''
func TestSabyLineReadersAcceptInternalFieldAliases(t *testing.T) {
\tt.Parallel()
\tvalue := map[string]any{
\t\t"_type": "recordset",
\t\t"s": []any{map[string]any{"n": "@Номенклатура"}, map[string]any{"n": "КоличествоОсн"}},
\t\t"d": []any{[]any{float64(3604), "3,0"}},
\t}
\tif count := sabyLineCount(value); count != 1 { t.Fatalf("count=%d", count) }
\tquantities := sabyLineQuantities(value)
\tif quantities[3604] != 3 { t.Fatalf("quantities=%+v", quantities) }
}
'''
if "TestSabyLineReadersAcceptInternalFieldAliases" not in text:
    text += alias_test
p.write_text(text)
