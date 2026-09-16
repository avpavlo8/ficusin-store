from pathlib import Path

repo = Path(__file__).resolve().parents[1]

def replace_once(path: Path, old: str, new: str):
    text = path.read_text()
    if old not in text:
        raise SystemExit(f"anchor not found in {path}: {old[:120]!r}")
    path.write_text(text.replace(old, new, 1))

# 1. Allow an explicit buyer decision to pair one missing plan row with one
# supplier-only invoice row. This deliberately reuses the existing line PATCH
# endpoint so old clients remain compatible.
model = repo / "backend/internal/procurement/model.go"
replace_once(model,
'''\tInvoiceExcluded   *bool    `json:"invoiceExcluded"`\n\tExclusionReason   *string  `json:"exclusionReason"`\n''',
'''\tInvoiceExcluded   *bool    `json:"invoiceExcluded"`\n\tExclusionReason   *string  `json:"exclusionReason"`\n\tInvoiceLineID     *int64   `json:"invoiceLineId"`\n''')

service = repo / "backend/internal/procurement/service.go"
replace_once(service,
'''\tif lineID <= 0 || (input.ExpectedUnitPrice == nil && input.PotDiameterCM == nil && input.HeightCM == nil && input.LoadUnit == nil && input.AcceptComparison == nil && input.ComparisonNote == nil && input.InvoiceExcluded == nil) {\n\t\treturn OrderDetail{}, ErrInvalidInput\n\t}\n''',
'''\tif lineID <= 0 || (input.ExpectedUnitPrice == nil && input.PotDiameterCM == nil && input.HeightCM == nil && input.LoadUnit == nil && input.AcceptComparison == nil && input.ComparisonNote == nil && input.InvoiceExcluded == nil && input.InvoiceLineID == nil) {\n\t\treturn OrderDetail{}, ErrInvalidInput\n\t}\n\tif input.InvoiceLineID != nil {\n\t\tif *input.InvoiceLineID <= 0 || input.ExpectedUnitPrice != nil || input.PotDiameterCM != nil || input.HeightCM != nil || input.LoadUnit != nil || input.AcceptComparison != nil || input.ComparisonNote != nil || input.InvoiceExcluded != nil {\n\t\t\treturn OrderDetail{}, ErrInvalidInput\n\t\t}\n\t}\n''')

# 2. Pending supplier-only rows are review candidates, not part of the active
# purchase until the buyer explicitly accepts them as a new position.
cat = repo / "backend/internal/procurement/postgres_catalogue.go"
text = cat.read_text()
text = text.replace(
'''\t\tLEFT JOIN procurement_order_lines l ON l.procurement_order_id = o.id\n\t\t\tAND l.reconciliation_status <> 'superseded' AND NOT l.invoice_excluded\n''',
'''\t\tLEFT JOIN procurement_order_lines l ON l.procurement_order_id = o.id\n\t\t\tAND l.reconciliation_status <> 'superseded' AND NOT l.invoice_excluded\n\t\t\tAND (l.reconciliation_status <> 'added' OR l.comparison_accepted)\n''', 1)
text = text.replace(
'''\t\t\t\tWHERE l.saby_id IS NOT NULL AND l.match_status = 'confirmed'\n\t\t\t\t\tAND o.status IN ('ordered', 'invoice_received', 'review', 'ready_to_receive')\n''',
'''\t\t\t\tWHERE l.saby_id IS NOT NULL AND l.match_status = 'confirmed'\n\t\t\t\t\tAND NOT l.invoice_excluded AND l.reconciliation_status <> 'superseded'\n\t\t\t\t\tAND (l.reconciliation_status <> 'added' OR l.comparison_accepted)\n\t\t\t\t\tAND o.status IN ('ordered', 'invoice_received', 'review', 'ready_to_receive')\n''', 1)
cat.write_text(text)

actions = repo / "backend/internal/procurement/postgres_actions.go"
replace_once(actions,
'''\t\tLEFT JOIN procurement_order_lines l ON l.procurement_order_id = o.id\n\t\t\tAND l.reconciliation_status <> 'superseded' AND NOT l.invoice_excluded\n\t\tWHERE o.id = $1\n''',
'''\t\tLEFT JOIN procurement_order_lines l ON l.procurement_order_id = o.id\n\t\t\tAND l.reconciliation_status <> 'superseded' AND NOT l.invoice_excluded\n\t\t\tAND (l.reconciliation_status <> 'added' OR l.comparison_accepted)\n\t\tWHERE o.id = $1\n''')

pg = repo / "backend/internal/procurement/postgres.go"
text = pg.read_text()
text = text.replace(
'''\t\tFROM procurement_order_lines WHERE procurement_order_id = $1\n\t\t\tAND reconciliation_status<>'superseded' AND NOT invoice_excluded FOR UPDATE\n''',
'''\t\tFROM procurement_order_lines WHERE procurement_order_id = $1\n\t\t\tAND reconciliation_status<>'superseded' AND NOT invoice_excluded\n\t\t\tAND (reconciliation_status<>'added' OR comparison_accepted) FOR UPDATE\n''', 1)
# A missing plan price is not proof of a match. It must stay in review rather
# than silently becoming "Совпадает".
text = text.replace(
'''\t\t\t\t\treconciliation_status=CASE WHEN ordered_qty<>$7 OR\n\t\t\t\t\t\t(expected_unit_price IS NOT NULL AND ABS(expected_unit_price-$8)>.005)\n\t\t\t\t\t\tTHEN 'changed' ELSE 'matched' END,updated_at = CURRENT_TIMESTAMP\n''',
'''\t\t\t\t\treconciliation_status=CASE WHEN ordered_qty<>$7 OR expected_unit_price IS NULL OR $8 IS NULL OR\n\t\t\t\t\t\tABS(expected_unit_price-$8)>.005\n\t\t\t\t\t\tTHEN 'changed' ELSE 'matched' END,updated_at = CURRENT_TIMESTAMP\n''', 1)
pg.write_text(text)

# 3. Explicit pair operation. It works even when the supplier alias is already
# confirmed to a wrong Saby card, which is exactly the state where the global
# alias review list no longer shows the row.
cat = repo / "backend/internal/procurement/postgres_catalogue.go"
text = cat.read_text()
old_case = '''\t\t\treconciliation_status=CASE WHEN planned.ordered_qty IS DISTINCT FROM invoice.invoiced_qty\n\t\t\t\tOR (planned.expected_unit_price IS NOT NULL AND\n\t\t\t\t\t(invoice.unit_price IS NULL OR ABS(planned.expected_unit_price-invoice.unit_price)>.005))\n\t\t\t\tTHEN 'changed' ELSE 'matched' END,\n'''
new_case = '''\t\t\treconciliation_status=CASE WHEN planned.ordered_qty IS DISTINCT FROM invoice.invoiced_qty\n\t\t\t\tOR planned.expected_unit_price IS NULL OR invoice.unit_price IS NULL\n\t\t\t\tOR ABS(planned.expected_unit_price-invoice.unit_price)>.005\n\t\t\t\tTHEN 'changed' ELSE 'matched' END,\n'''
if old_case not in text:
    raise SystemExit("late alias comparison anchor not found")
text = text.replace(old_case, new_case, 1)
text = text.replace(
'''\t\t\t\tWHEN invoiced_qty IS DISTINCT FROM ordered_qty OR\n\t\t\t\t\t(expected_unit_price IS NOT NULL AND ABS(expected_unit_price-unit_price)>.005) THEN 'changed'\n''',
'''\t\t\t\tWHEN invoiced_qty IS DISTINCT FROM ordered_qty OR expected_unit_price IS NULL OR unit_price IS NULL\n\t\t\t\t\tOR ABS(expected_unit_price-unit_price)>.005 THEN 'changed'\n''', 1)

branch_anchor = '''\tif err != nil {\n\t\treturn OrderDetail{}, fmt.Errorf("lock procurement line: %w", err)\n\t}\n\t_, err = tx.Exec(ctx, `\n'''
branch = r'''\tif err != nil {
		return OrderDetail{}, fmt.Errorf("lock procurement line: %w", err)
	}
	if input.InvoiceLineID != nil {
		if err := pairInvoiceLineWithPlan(ctx, tx, actor.CustomerID, orderID, lineID, *input.InvoiceLineID); err != nil {
			return OrderDetail{}, err
		}
		if err := rebalanceInvoiceAllocations(ctx, tx, orderID); err != nil {
			return OrderDetail{}, fmt.Errorf("rebalance requests after manual invoice match: %w", err)
		}
	} else {
	_, err = tx.Exec(ctx, `
'''
if branch_anchor not in text:
    raise SystemExit("UpdateOrderLine branch anchor not found")
text = text.replace(branch_anchor, branch, 1)
close_anchor = '''\tif err != nil {\n\t\treturn OrderDetail{}, fmt.Errorf("update procurement line: %w", err)\n\t}\n\tif input.InvoiceExcluded != nil {\n'''
close_new = '''\tif err != nil {\n\t\treturn OrderDetail{}, fmt.Errorf("update procurement line: %w", err)\n\t}\n\t}\n\tif input.InvoiceLineID == nil && input.InvoiceExcluded != nil {\n'''
if close_anchor not in text:
    raise SystemExit("UpdateOrderLine close anchor not found")
text = text.replace(close_anchor, close_new, 1)

helper_anchor = '\nfunc (store *PostgresStore) UpdateOrderLine('
helper = r'''
func pairInvoiceLineWithPlan(ctx context.Context, tx pgx.Tx, actorID, orderID, plannedID, invoiceID int64) error {
	var plannedSaby string
	var plannedCanonical int64
	var aliasID, documentID int64
	var sourceLine int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(planned.saby_id,''),COALESCE(planned.canonical_variant_id,
			(SELECT variant_id FROM canonical_product_directory WHERE active AND saby_id=planned.saby_id ORDER BY variant_id LIMIT 1),0),
			invoice.supplier_alias_id,invoice.procurement_document_id,invoice.source_line
		FROM procurement_order_lines planned
		JOIN procurement_order_lines invoice ON invoice.procurement_order_id=planned.procurement_order_id
		WHERE planned.id=$1 AND planned.procurement_order_id=$2
			AND planned.ordered_qty>0 AND planned.procurement_document_id IS NULL
			AND planned.reconciliation_status='missing' AND NOT planned.invoice_excluded
			AND invoice.id=$3 AND invoice.ordered_qty=0 AND invoice.procurement_document_id IS NOT NULL
			AND invoice.source_line IS NOT NULL AND invoice.supplier_alias_id IS NOT NULL
			AND invoice.reconciliation_status='added' AND NOT invoice.invoice_excluded
		FOR UPDATE OF planned,invoice
	`, plannedID, orderID, invoiceID).Scan(&plannedSaby, &plannedCanonical, &aliasID, &documentID, &sourceLine)
	if errors.Is(err, pgx.ErrNoRows) {
		return &UserFacingError{Message: "Эти строки уже нельзя сопоставить: обновите закупку и выберите две актуальные строки"}
	}
	if err != nil {
		return fmt.Errorf("lock manual procurement reconciliation pair: %w", err)
	}
	if plannedSaby != "" {
		if _, err := tx.Exec(ctx, `UPDATE procurement_supplier_aliases SET
			matched_saby_id=$2,canonical_variant_id=NULLIF($3,0),match_status='confirmed',confidence=1,updated_at=CURRENT_TIMESTAMP
			WHERE id=$1`, aliasID, plannedSaby, plannedCanonical); err != nil {
			return fmt.Errorf("confirm supplier alias from manual invoice match: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO procurement_supplier_products(
			supplier_id,saby_id,canonical_variant_id,supplier_article,availability_status,updated_by)
			SELECT supplier_id,$2,NULLIF($3,0),supplier_article,'available',NULLIF($4,0)
			FROM procurement_supplier_aliases WHERE id=$1
			ON CONFLICT (supplier_id,saby_id) DO UPDATE SET
				canonical_variant_id=COALESCE(EXCLUDED.canonical_variant_id,procurement_supplier_products.canonical_variant_id),
				supplier_article=CASE WHEN EXCLUDED.supplier_article<>'' THEN EXCLUDED.supplier_article ELSE procurement_supplier_products.supplier_article END,
				availability_status='available',updated_by=EXCLUDED.updated_by,updated_at=CURRENT_TIMESTAMP`,
			aliasID, plannedSaby, plannedCanonical, actorID); err != nil {
			return fmt.Errorf("remember supplier product from manual invoice match: %w", err)
		}
	}
	// Release the document/source-line unique key before transferring invoice
	// provenance to the buyer's original planned row.
	if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines SET procurement_document_id=NULL,
		reconciliation_status='superseded',updated_at=CURRENT_TIMESTAMP WHERE id=$1`, invoiceID); err != nil {
		return fmt.Errorf("supersede manually matched supplier line: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE procurement_order_lines planned SET
		procurement_document_id=$3,supplier_alias_id=invoice.supplier_alias_id,
		canonical_variant_id=CASE WHEN $5>0 THEN $5 ELSE COALESCE(invoice.canonical_variant_id,planned.canonical_variant_id) END,
		invoice_raw_name=invoice.invoice_raw_name,invoice_supplier_article=invoice.invoice_supplier_article,
		invoiced_qty=invoice.invoiced_qty,unit_price=invoice.unit_price,line_total=invoice.line_total,
		match_status=CASE WHEN $6<>'' THEN 'confirmed' ELSE invoice.match_status END,
		source_page=invoice.source_page,source_line=$4,comparison_accepted=FALSE,comparison_note='',
		reconciliation_status=CASE WHEN planned.ordered_qty IS DISTINCT FROM invoice.invoiced_qty
			OR planned.expected_unit_price IS NULL OR invoice.unit_price IS NULL
			OR ABS(planned.expected_unit_price-invoice.unit_price)>.005 THEN 'changed' ELSE 'matched' END,
		updated_at=CURRENT_TIMESTAMP
		FROM procurement_order_lines invoice WHERE planned.id=$1 AND invoice.id=$2`,
		plannedID, invoiceID, documentID, sourceLine, plannedCanonical, plannedSaby); err != nil {
		return fmt.Errorf("merge supplier invoice row into procurement plan: %w", err)
	}
	return nil
}
'''
if helper_anchor not in text:
    raise SystemExit("helper insertion anchor not found")
text = text.replace(helper_anchor, '\n' + helper + helper_anchor, 1)
cat.write_text(text)

# Correct already-existing false-positive matches where there was no plan price
# to compare against (the current Fittonia state falls into this safety repair
# when expected_unit_price was absent).
migration = repo / "timeweb/migrations/20260916_procurement_reconciliation_workflow.sql"
migration.write_text('''-- A row cannot be called matched when the buyer plan had no price to compare.\nUPDATE procurement_order_lines\nSET reconciliation_status='changed',comparison_accepted=FALSE,comparison_note='',updated_at=CURRENT_TIMESTAMP\nWHERE ordered_qty>0 AND procurement_document_id IS NOT NULL\n  AND reconciliation_status='matched' AND expected_unit_price IS NULL\n  AND NOT invoice_excluded;\n''')

# 4. Buyer-facing workflow: pending supplier lines live in a separate review
# pool; missing plan lines can pick directly from that pool; changed rows can
# be accepted in bulk.
ui = repo / "frontend/src/AdminProcurementDialogs.tsx"
text = ui.read_text()
text = text.replace(
'''  const [uploading, setUploading] = useState(false);\n''',
'''  const [uploading, setUploading] = useState(false);\n  const [pairingLineID, setPairingLineID] = useState<number | null>(null);\n''', 1)
func_anchor = '''  const setInvoiceExcluded = async (line: ProcurementOrderLine, excluded: boolean) => { const reason = excluded ? window.prompt("Почему строка исключена? Причина сохранится в истории.", line.invoiceExclusionReason || "Поставщик не включил позицию") : ""; if (excluded && !reason?.trim()) return; setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ invoiceExcluded: excluded, exclusionReason: reason || "" }) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };\n'''
func_new = func_anchor + '''  const acceptAllChanged = async () => { if (!detail) return; const lines = detail.lines.filter((line) => line.reconciliationStatus === "changed" && line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded); if (!lines.length || !window.confirm(`Принять все изменения по цене/количеству: ${lines.length}? Несопоставленные и отсутствующие позиции сюда не входят.`)) return; setSaving(true); try { await Promise.all(lines.map((line) => api(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ acceptComparison: true, comparisonNote: "Массово принято в сверке закупки" }) }))); await load(); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };\n  const matchInvoiceLine = async (plannedLine: ProcurementOrderLine, invoiceLine: ProcurementOrderLine) => { setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${plannedLine.id}`, { method: "PATCH", body: JSON.stringify({ invoiceLineId: invoiceLine.id }) }); setDetail(normalizeProcurementOrderDetail(item)); setPairingLineID(null); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };\n  const acceptAddedLine = async (line: ProcurementOrderLine) => { if (!window.confirm(`Добавить «${line.invoiceRawName || line.rawName}» в закупку как новую позицию поставщика?`)) return; setSaving(true); try { const item = await api<ProcurementOrderDetail>(`/api/v1/admin/procurement/order-lines/${line.id}`, { method: "PATCH", body: JSON.stringify({ acceptComparison: true, comparisonNote: "Подтверждено как новая позиция из инвойса" }) }); setDetail(normalizeProcurementOrderDetail(item)); onSaved(); } catch (error) { onError((error as Error).message); } finally { setSaving(false); } };\n'''
if func_anchor not in text:
    raise SystemExit("frontend function anchor not found")
text = text.replace(func_anchor, func_new, 1)
return_anchor = '''  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog procurement-order-dialog"'''
return_new = '''  const pendingInvoiceLines = detail?.lines.filter((line) => line.reconciliationStatus === "added" && !line.comparisonAccepted && !line.invoiceExcluded) || [];\n  const visibleLines = detail?.lines.filter((line) => line.reconciliationStatus !== "added" || line.comparisonAccepted || line.invoiceExcluded) || [];\n  const changedLines = detail?.lines.filter((line) => line.reconciliationStatus === "changed" && line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded) || [];\n  const pairingLine = pairingLineID == null ? null : detail?.lines.find((line) => line.id === pairingLineID) || null;\n  return <><button className="admin-dialog-backdrop" aria-label="Закрыть" onClick={onClose} /><div className="admin-dialog procurement-order-dialog"'''
if return_anchor not in text:
    raise SystemExit("frontend return anchor not found")
text = text.replace(return_anchor, return_new, 1)
checklist_anchor = '''      <section className={detail.validation.blockers?.length ? "procurement-checklist blocked" : "procurement-checklist ready"}><strong>{detail.validation.blockers?.length ? "Что нужно сделать дальше" : "Проверки пройдены"}</strong>{detail.validation.blockers?.length ? <ul>{detail.validation.blockers.map((blocker) => <li key={blocker}>{blocker === "Не загружен инвойс или счёт" ? "Загрузите PDF-инвойс кнопкой выше — он будет привязан именно к этой закупке" : blocker}</li>)}</ul> : <p>Инвойс, сопоставление, размеры и расхождения проверены.</p>}<small>Красные строки означают расхождение с инвойсом или отсутствие связи с товаром СБИС; сохранённые данные закупки при этом не теряются.</small><small>Телег: {detail.validation.trolleyCount} · распределено {money.format(detail.validation.allocatedTrolleyRub)} из {money.format(detail.validation.expectedTrolleyRub)} · Москва → Рязань {money.format(detail.validation.allocatedRyazanRub)} из {money.format(detail.validation.expectedRyazanRub)}</small></section>\n'''
checklist_new = checklist_anchor + '''      {changedLines.length > 0 && <div className="procurement-batch-buttons"><button disabled={saving} onClick={() => void acceptAllChanged()}>Принять все изменения ({changedLines.length})</button><small>Только изменения количества/цены. «Нет в инвойсе» и новые строки нужно разобрать отдельно.</small></div>}\n      {pairingLine && <section className="procurement-checklist blocked"><strong>Выберите строку инвойса для «{pairingLine.sabyName || pairingLine.rawName}»</strong><p>После выбора данные инвойса перейдут в исходную строку закупки. Плановая цена и количество сохранятся для сравнения.</p><div className="admin-table-wrap"><table className="admin-table procurement-lines"><thead><tr><th>Строка инвойса</th><th>Количество</th><th>Цена</th><th></th></tr></thead><tbody>{pendingInvoiceLines.map((invoiceLine) => <tr key={invoiceLine.id}><td><strong>{invoiceLine.invoiceRawName || invoiceLine.rawName}</strong><small>{invoiceLine.invoiceSupplierArticle || "Артикул не указан"}</small></td><td>{invoiceLine.invoicedQuantity ?? "—"}</td><td>{invoiceLine.unitPrice.toFixed(2)} {detail.order.currency}</td><td><button disabled={saving} onClick={() => void matchInvoiceLine(pairingLine, invoiceLine)}>Сопоставить</button></td></tr>)}</tbody></table></div><div className="dialog-actions"><button onClick={() => setPairingLineID(null)}>Отмена</button></div></section>}\n'''
if checklist_anchor not in text:
    raise SystemExit("frontend checklist anchor not found")
text = text.replace(checklist_anchor, checklist_new, 1)
text = text.replace('''<tbody>{detail.lines.map((line) => <tr key={line.id}''', '''<tbody>{visibleLines.map((line) => <tr key={line.id}''', 1)
old_actions = '''{line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded && <button onClick={() => void acceptMismatch(line)}>Принять расхождение</button>}{["missing","added","changed"].includes(line.reconciliationStatus) && !line.invoiceExcluded && <button onClick={() => void setInvoiceExcluded(line, true)}>Исключить</button>}'''
new_actions = '''{line.reconciliationStatus === "changed" && line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded && <button onClick={() => void acceptMismatch(line)}>Принять расхождение</button>}{line.reconciliationStatus === "missing" && !line.invoiceExcluded && pendingInvoiceLines.length > 0 && <button onClick={() => setPairingLineID(line.id)}>Выбрать из инвойса</button>}{["missing","added","changed"].includes(line.reconciliationStatus) && !line.invoiceExcluded && <button onClick={() => void setInvoiceExcluded(line, true)}>Исключить</button>}'''
if old_actions not in text:
    raise SystemExit("frontend row actions anchor not found")
text = text.replace(old_actions, new_actions, 1)
table_end = '''      <div className="admin-table-wrap"><table className="admin-table procurement-lines"><thead><tr><th>План</th><th>Инвойс</th><th>Сверка</th><th>Телега / размер</th><th>Упаковки</th><th>Количество</th><th>Цена план / факт</th><th>Себестоимость</th><th></th></tr></thead><tbody>{visibleLines.map((line) => <tr key={line.id} className={line.comparisonMismatch && !line.comparisonAccepted ? "procurement-row-mismatch" : ""}><td><strong>{line.sabyName || line.rawName}</strong><small>{[line.supplierCategory,line.supplierArticle].filter(Boolean).join(" · ") || "Артикул не заполнен"}</small></td><td><strong>{line.invoiceRawName || (line.reconciliationStatus === "missing" ? "Позиция отсутствует" : "—")}</strong><small>{line.invoiceSupplierArticle || "Артикул в PDF не указан"}</small></td><td><span className={`procurement-reconciliation procurement-reconciliation-${line.reconciliationStatus}`}>{reconciliationLabel(line.reconciliationStatus)}</span>{line.invoiceExclusionReason && <small>{line.invoiceExclusionReason}</small>}{line.comparisonAccepted && <small className="procurement-ok">Расхождение принято</small>}</td><td>{line.loadUnit || "—"}<small>{[line.potDiameterCm && `D${line.potDiameterCm}`, line.heightCm && `${line.heightCm} см`].filter(Boolean).join(" · ") || "Размер не заполнен"}</small></td><td>{line.packageCount && line.unitsPerPackage ? `${line.packageCount} × ${line.unitsPerPackage}` : "—"}</td><td>{line.orderedQuantity || "—"} / {line.invoicedQuantity ?? "—"}</td><td>{line.expectedUnitPrice ? line.expectedUnitPrice.toFixed(2) : "—"} / {line.invoicedQuantity == null ? "—" : line.unitPrice.toFixed(2)} {detail.order.currency}</td><td>{line.unitCostRub == null ? "—" : money.format(line.unitCostRub)}<small>{line.currentUnitCostRub == null ? "Текущая: неизвестно" : `Текущая: ${money.format(line.currentUnitCostRub)} · ${line.currentUnitCostKind === "actual" ? "фактическая" : "оценочная"}`}</small></td><td><div className="procurement-inline-actions"><button onClick={() => void editLine(line)}>Размеры</button>{line.reconciliationStatus === "changed" && line.comparisonMismatch && !line.comparisonAccepted && !line.invoiceExcluded && <button onClick={() => void acceptMismatch(line)}>Принять расхождение</button>}{line.reconciliationStatus === "missing" && !line.invoiceExcluded && pendingInvoiceLines.length > 0 && <button onClick={() => setPairingLineID(line.id)}>Выбрать из инвойса</button>}{["missing","added","changed"].includes(line.reconciliationStatus) && !line.invoiceExcluded && <button onClick={() => void setInvoiceExcluded(line, true)}>Исключить</button>}{line.invoiceExcluded && <button onClick={() => void setInvoiceExcluded(line, false)}>Вернуть в сверку</button>}</div></td></tr>)}</tbody></table></div>\n'''
pending_section = table_end + '''      {pendingInvoiceLines.length > 0 && <section className="procurement-checklist blocked"><strong>Несопоставленные строки инвойса: {pendingInvoiceLines.length}</strong><p>Они пока не считаются частью закупки. Сопоставьте их с позицией «Нет в инвойсе» кнопкой выше либо подтвердите, что поставщик действительно добавил новую позицию.</p><div className="admin-table-wrap"><table className="admin-table procurement-lines"><thead><tr><th>Инвойс</th><th>Количество</th><th>Цена</th><th>Связь с СБИС</th><th></th></tr></thead><tbody>{pendingInvoiceLines.map((line) => <tr key={line.id}><td><strong>{line.invoiceRawName || line.rawName}</strong><small>{line.invoiceSupplierArticle || "Артикул не указан"}</small></td><td>{line.invoicedQuantity ?? "—"}</td><td>{line.unitPrice.toFixed(2)} {detail.order.currency}</td><td>{line.sabyName || (line.matchStatus === "confirmed" ? line.sabyId : "Не сопоставлено")}</td><td><button disabled={saving} onClick={() => void acceptAddedLine(line)}>Это новая позиция</button></td></tr>)}</tbody></table></div></section>}\n'''
if table_end not in text:
    raise SystemExit("frontend table end anchor not found")
text = text.replace(table_end, pending_section, 1)
ui.write_text(text)

# 5. Live regression: explicit pairing must override a wrong confirmed alias,
# preserve buyer plan values and keep the price difference visible.
test = repo / "backend/internal/procurement/late_alias_reconciliation_live_test.go"
text = test.read_text()
append = r'''

func TestManualInvoicePairOverridesWrongAliasAndPreservesPlanPrice(t *testing.T) {
	dsn := os.Getenv("CRM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CRM_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewPostgresStore(pool)
	unique := time.Now().UnixNano()
	plannedSaby := fmt.Sprintf("manual-carmona-%d", unique)
	wrongSaby := fmt.Sprintf("manual-wrong-%d", unique)
	for id, name := range map[string]string{plannedSaby: "Bonsai Carmona D10", wrongSaby: "Wrong card"} {
		if _, err = pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,name,balance,section_path,seen_at)
			VALUES($1,$1,$2,0,ARRAY['Цветы'],CURRENT_TIMESTAMP)`, id, name); err != nil {
			t.Fatal(err)
		}
	}
	var actorID int64
	if err = pool.QueryRow(ctx, `INSERT INTO customers(email,phone,password_hash,full_name,consent_at)
		VALUES($1,$2,'','Manual pair owner',CURRENT_TIMESTAMP) RETURNING id`, fmt.Sprintf("manual-pair-%d@example.invalid", unique), fmt.Sprintf("+77%09d", unique%1000000000)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var supplierID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_suppliers(name,kind,country_code,default_currency)
		VALUES($1,'international','NL','EUR') RETURNING id`, fmt.Sprintf("Manual pair supplier %d", unique)).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	var orderID, plannedID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_orders(supplier_id,order_number,source_kind,currency,status,created_by)
		VALUES($1,$2,'recommendation','EUR','ordered',$3) RETURNING id`, supplierID, fmt.Sprintf("PAIR-%d", unique), actorID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,saby_id,raw_name,ordered_qty,
		expected_unit_price,load_unit,pot_diameter_cm,height_cm,match_status,reconciliation_status)
		VALUES($1,$2,'Bonsai Carmona D10',10,4.25,'shelf',10,30,'confirmed','missing') RETURNING id`, orderID, plannedSaby).Scan(&plannedID); err != nil {
		t.Fatal(err)
	}
	var aliasID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_supplier_aliases(supplier_id,raw_name,normalized_name,matched_saby_id,match_status,confidence,occurrences,last_seen_at)
		VALUES($1,'Bakarnie Nalina','bakarnie nalina',$2,'confirmed',1,1,CURRENT_DATE) RETURNING id`, supplierID, wrongSaby).Scan(&aliasID); err != nil {
		t.Fatal(err)
	}
	var documentID int64
	hash := fmt.Sprintf("%064x", unique)
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_documents(supplier_id,procurement_order_id,file_name,content_type,size_bytes,sha256,content,
		parser_kind,parser_version,parse_status,arithmetic_status,document_number,currency,line_count,unit_count,created_by)
		VALUES($1,$2,'manual-pair.pdf','application/pdf',8,$3,'12345678','holland_packing_list',1,'review','ok',$4,'EUR',1,12,$5) RETURNING id`,
		supplierID, orderID, hash, fmt.Sprintf("INV-PAIR-%d", unique), actorID).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	var addedID int64
	if err = pool.QueryRow(ctx, `INSERT INTO procurement_order_lines(procurement_order_id,procurement_document_id,supplier_alias_id,saby_id,
		raw_name,invoice_raw_name,invoiced_qty,unit_price,line_total,load_unit,match_status,source_page,source_line,reconciliation_status)
		VALUES($1,$2,$3,$4,'Bakarnie Nalina','Bakarnie Nalina',12,4.50,54,'shelf','confirmed',1,1,'added') RETURNING id`,
		orderID, documentID, aliasID, wrongSaby).Scan(&addedID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateOrderLine(ctx, Actor{CustomerID: actorID, Role: "owner"}, plannedID, OrderLineUpdate{InvoiceLineID: &addedID}); err != nil {
		t.Fatal(err)
	}
	var status, mappedSaby, invoiceName string
	var expected, actual float64
	var qty int
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status,expected_unit_price::DOUBLE PRECISION,unit_price::DOUBLE PRECISION,invoiced_qty,invoice_raw_name
		FROM procurement_order_lines WHERE id=$1`, plannedID).Scan(&status, &expected, &actual, &qty, &invoiceName); err != nil {
		t.Fatal(err)
	}
	if status != "changed" || expected != 4.25 || actual != 4.5 || qty != 12 || invoiceName != "Bakarnie Nalina" {
		t.Fatalf("paired line status=%q expected=%v actual=%v qty=%d invoice=%q", status, expected, actual, qty, invoiceName)
	}
	if err = pool.QueryRow(ctx, `SELECT matched_saby_id FROM procurement_supplier_aliases WHERE id=$1`, aliasID).Scan(&mappedSaby); err != nil || mappedSaby != plannedSaby {
		t.Fatalf("alias mapped=%q err=%v", mappedSaby, err)
	}
	if err = pool.QueryRow(ctx, `SELECT reconciliation_status FROM procurement_order_lines WHERE id=$1`, addedID).Scan(&status); err != nil || status != "superseded" {
		t.Fatalf("supplier-only line status=%q err=%v", status, err)
	}
}
'''
if 'TestManualInvoicePairOverridesWrongAliasAndPreservesPlanPrice' not in text:
    test.write_text(text + append)
