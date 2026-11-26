import { procurementParserLabels } from "./AdminProcurement";
import type { ProcurementDocument } from "./adminTypes";
import { quantityLabel } from "./procurementUX";

export default function ProcurementOrderDocuments({
  documents,
  closed,
  saving,
  uploading,
  arrivalDate,
  expectedArrivalDate,
  onArrivalDateChange,
  onSaveArrivalDate,
  onUpload,
}: {
  documents: ProcurementDocument[];
  closed: boolean;
  saving: boolean;
  uploading: boolean;
  arrivalDate: string | null;
  expectedArrivalDate?: string;
  onArrivalDateChange: (date: string) => void;
  onSaveArrivalDate: () => void;
  onUpload: (file: File | null) => void;
}) {
  return (
    <section className="procurement-order-documents" aria-label="Документы закупки">
      <div className="procurement-documents-heading">
        <h3>
          Документы <span>{documents.length}</span>
        </h3>
        <label className="secondary-button procurement-inline-upload">
          {uploading
            ? "Разбираем PDF…"
            : documents.length
              ? "Загрузить новую версию"
              : "Загрузить инвойс / счёт"}
          <input
            type="file"
            accept="application/pdf,.pdf"
            disabled={closed || saving || uploading}
            onChange={(event) => onUpload(event.target.files?.[0] || null)}
          />
        </label>
      </div>
      {!closed && (
        <div className="procurement-arrival-fields admin-form-grid">
          <label>
            Ориентировочная дата поступления
            <input
              type="date"
              value={arrivalDate ?? expectedArrivalDate ?? ""}
              onChange={(event) => onArrivalDateChange(event.target.value)}
              disabled={saving || uploading}
            />
          </label>
          <button
            type="button"
            disabled={
              saving ||
              uploading ||
              arrivalDate === null ||
              arrivalDate === (expectedArrivalDate ?? "")
            }
            onClick={() => onSaveArrivalDate()}
          >
            Сохранить дату
          </button>
        </div>
      )}
      {documents.length ? (
        <ul className="procurement-document-list">
          {[...documents]
            .sort(
              (a, b) =>
                (b.revisionNo || 1) - (a.revisionNo || 1) || b.createdAt.localeCompare(a.createdAt),
            )
            .map((document) => (
              <li key={document.id}>
                <div>
                  <a
                    href={`/api/v1/admin/procurement/documents/${document.id}/source.pdf`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {document.documentNumber
                      ? `${document.parserKind === "domestic_payment_invoice" ? "Счёт" : "Инвойс"} № ${document.documentNumber}`
                      : document.fileName}{" "}
                    · PDF
                  </a>
                  <small>
                    {document.documentDate
                      ? new Date(document.documentDate).toLocaleDateString("ru-RU")
                      : new Date(document.createdAt).toLocaleDateString("ru-RU")}{" "}
                    · версия {document.revisionNo || 1} ·{" "}
                    {document.superseded ? "заменена" : "действующая"}
                  </small>
                </div>
                <span
                  className={
                    document.arithmeticStatus === "ok" ? "procurement-ok" : "procurement-warning"
                  }
                >
                  {document.arithmeticStatus === "ok" ? "Суммы сходятся" : "Проверить суммы"}
                </span>
                <details>
                  <summary>Суммы и разбор</summary>
                  <p>
                    {procurementParserLabels[document.parserKind] || document.parserKind} ·{" "}
                    {quantityLabel(document.lines)} · {document.units} шт.
                  </p>
                  <p>
                    Растения: {document.productSubtotal.toLocaleString("ru-RU")} {document.currency}{" "}
                    · упаковка: {document.packageTotal.toLocaleString("ru-RU")} {document.currency}{" "}
                    · всего: {document.documentTotal.toLocaleString("ru-RU")} {document.currency}
                  </p>
                  <p>
                    {document.parseStatus === "review"
                      ? "Есть несопоставленные строки"
                      : "Разобрано"}
                  </p>
                  {document.parseError && (
                    <p className="procurement-warning">{document.parseError}</p>
                  )}
                </details>
              </li>
            ))}
        </ul>
      ) : (
        <p>Инвойс или счёт будет храниться здесь вместе с закупкой.</p>
      )}
    </section>
  );
}
