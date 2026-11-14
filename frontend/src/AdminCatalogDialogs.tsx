import { AdminProductSearch } from "./AdminProductSearch";
import { NumericInput } from "./NumericInput";
import { useEffect, useState } from "react";
import { CategoryPicker } from "./CategoryPicker";
import { Dialog, api, money } from "./adminShared";
import type { AdminVariant } from "./AdminPim";
import type { Category, CategoryAttribute, ImportEntry, Product } from "./adminTypes";
import {
  canReconcileMergeContent,
  mergeContentFields,
  mergeContentPatch,
} from "./publishedMergeContent";

export function AttributeFields({
  schema,
  values,
  onChange,
}: {
  schema: CategoryAttribute[];
  values: Record<string, unknown>;
  onChange: (code: string, value: string | number | boolean | string[] | null) => void;
}) {
  const field = (attribute: CategoryAttribute) => {
    const value = values[attribute.code];
    const title = `${attribute.name}${attribute.unit ? `, ${attribute.unit}` : ""}${attribute.required ? " *" : ""}`;
    if (attribute.dataType === "boolean")
      return (
        <label className="attribute-control" key={attribute.code}>
          <span>{title}</span>
          <select
            aria-label={title}
            value={value == null ? "" : String(value)}
            onChange={(event) =>
              onChange(
                attribute.code,
                event.target.value === "" ? null : event.target.value === "true",
              )
            }
          >
            <option value="">Не указано</option>
            <option value="true">Да</option>
            <option value="false">Нет</option>
          </select>
        </label>
      );
    if (attribute.dataType === "enum") {
      const current = String(value ?? "");
      const unknown = current && !attribute.options.includes(current);
      return (
        <label className="attribute-control" key={attribute.code}>
          <span>{title}</span>
          <select
            aria-label={title}
            required={attribute.required}
            value={current}
            onChange={(event) => onChange(attribute.code, event.target.value || null)}
          >
            <option value="">Не указано</option>
            {unknown && <option value={current}>{`Неизвестное значение: ${current}`}</option>}
            {attribute.options.map((option) => (
              <option key={option} value={option}>
                {attribute.optionLabels?.[option] || option}
              </option>
            ))}
          </select>
          {unknown && (
            <small className="admin-inline-error">
              Старое значение сохранено. Выберите вариант из матрицы, чтобы заменить его.
            </small>
          )}
        </label>
      );
    }
    if (attribute.dataType === "multi_enum") {
      const selected = Array.isArray(value) ? value.map(String) : [];
      const unknown = selected.filter((option) => !attribute.options.includes(option));
      return (
        <fieldset className="attribute-multi" key={attribute.code}>
          <legend>{title}</legend>
          <div className="attribute-choice-list">
            {attribute.options.map((option) => (
              <label className={selected.includes(option) ? "selected" : ""} key={option}>
                <input
                  type="checkbox"
                  checked={selected.includes(option)}
                  onChange={(event) =>
                    onChange(
                      attribute.code,
                      event.target.checked
                        ? [...selected, option]
                        : selected.filter((item) => item !== option),
                    )
                  }
                />
                {attribute.optionLabels?.[option] || option}
              </label>
            ))}
          </div>
          {unknown.length > 0 && (
            <small className="admin-inline-error">
              Неизвестные старые значения сохранены: {unknown.join(", ")}
            </small>
          )}
        </fieldset>
      );
    }
    return (
      <label className="attribute-control" key={attribute.code}>
        <span>{title}</span>
        <NumericInput
          aria-label={title}
          type={attribute.dataType === "number" ? "number" : "text"}
          min={attribute.dataType === "number" ? 0 : undefined}
          step={attribute.dataType === "number" ? "any" : undefined}
          required={attribute.required}
          value={value == null ? "" : String(value)}
          onChange={(event) =>
            onChange(
              attribute.code,
              event.target.value === ""
                ? null
                : attribute.dataType === "number"
                  ? Number(event.target.value)
                  : event.target.value,
            )
          }
        />
      </label>
    );
  };
  const group = (attribute: CategoryAttribute) => {
    if (/^(package_|shipping_)/.test(attribute.code)) return "Упаковка и логистика";
    if (/(height|width|length|diameter|pot|size)/.test(attribute.code)) return "Размеры растения";
    if (/(light|water|care|humidity|temperature|soil|pet|toxic|growth)/.test(attribute.code))
      return "Уход";
    return "Характеристики";
  };
  const groups = ["Уход", "Размеры растения", "Упаковка и логистика", "Характеристики"]
    .map((title) => ({ title, items: schema.filter((item) => group(item) === title) }))
    .filter((item) => item.items.length > 0);
  return (
    <div className="attribute-groups wide">
      {groups.map(({ title, items }) => (
        <div className="attribute-group" key={title}>
          <h4>{title}</h4>
          <div className="attribute-control-grid">{items.map(field)}</div>
        </div>
      ))}
    </div>
  );
}

export type ProductEditorSection =
  | "main"
  | "attributes"
  | "care"
  | "variants"
  | "relationships"
  | "photos";
export function ImportDialog({
  onClose,
  onImported,
  onError,
}: {
  onClose: () => void;
  onImported: () => void;
  onError: (value: string) => void;
}) {
  const [codes, setCodes] = useState("");
  const [categoryId, setCategoryId] = useState<number | undefined>(undefined);
  const [categories, setCategories] = useState<Category[]>([]);
  const [preview, setPreview] = useState<ImportEntry[] | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    api<{ categories: Category[] }>("/api/v1/admin/categories")
      .then((data) => setCategories(data.categories))
      .catch((error) => onError(error.message));
  }, [onError]);
  const send = async (dryRun: boolean) => {
    setBusy(true);
    try {
      const result = await api<{ created: number; entries: ImportEntry[] }>(
        "/api/v1/admin/products/import",
        {
          method: "POST",
          body: JSON.stringify({ codes: [codes], categoryId, dryRun }),
        },
      );
      if (dryRun) {
        setPreview(result.entries);
      } else {
        onImported();
        return;
      }
    } catch (error) {
      onError((error as Error).message);
    }
    setBusy(false);
  };
  const found = preview ? preview.filter((entry) => entry.status === "new").length : 0;
  const labels: Record<string, string> = {
    new: "Черновик",
    exists: "Уже есть",
    missing: "Не найден",
  };
  return (
    <Dialog title="Импорт товаров из СБИС" onClose={onClose}>
      <label className="wide">
        Коды товаров
        <textarea
          rows={6}
          value={codes}
          onChange={(event) => {
            setCodes(event.target.value);
            setPreview(null);
          }}
          placeholder="X1150532&#10;X1150533"
        />
      </label>
      <p className="admin-hint">
        Вставьте коды из СБИС — через запятую, пробел или с новой строки. Подойдёт также артикул или
        штрихкод. Новые позиции создаются черновиками и не попадут на витрину, пока вы не приведёте
        название и варианты в порядок.
      </p>
      <div className="admin-field">
        <span className="admin-field-label">Раздел каталога</span>
        <CategoryPicker categories={categories} value={categoryId} onChange={setCategoryId} />
      </div>
      {preview && (
        <div className="admin-table-wrap">
          <table className="admin-table">
            <thead>
              <tr>
                <th>Код</th>
                <th>Товар</th>
                <th>Цена</th>
                <th>Остаток</th>
                <th>Что будет</th>
              </tr>
            </thead>
            <tbody>
              {preview.map((entry) => (
                <tr key={entry.code}>
                  <td>{entry.code}</td>
                  <td>{entry.name || "—"}</td>
                  <td>{entry.name ? money.format(entry.price) : "—"}</td>
                  <td>{entry.name ? entry.stock : "—"}</td>
                  <td>
                    <span className={"admin-pill " + entry.status}>
                      {labels[entry.status] || entry.status}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {preview && found === 0 && (
        <p className="admin-hint">Заводить нечего: ни одного нового товара в списке нет.</p>
      )}
      <div className="dialog-actions">
        <button onClick={onClose}>Отмена</button>
        <button disabled={busy || codes.trim() === ""} onClick={() => send(true)}>
          Проверить
        </button>
        <button
          className="primary"
          disabled={busy || !preview || found === 0}
          onClick={() => send(false)}
        >
          Создать черновики {found > 0 ? found : ""}
        </button>
      </div>
    </Dialog>
  );
}

export function MergeProductsDialog({
  products,
  onClose,
  onMerged,
  onError,
}: {
  products: Product[];
  onClose: () => void;
  onMerged: () => void;
  onError: (value: string) => void;
}) {
  const [targetId, setTargetId] = useState(products[0]?.id || 0);
  const [busy, setBusy] = useState(false);
  const target = products.find((item) => item.id === targetId);
  const sources = products.filter((item) => item.id !== targetId);
  const merge = async () => {
    setBusy(true);
    try {
      await api("/api/v1/admin/products/merge", {
        method: "POST",
        body: JSON.stringify({
          targetProductId: targetId,
          sourceProductIds: sources.map((item) => item.id),
        }),
      });
      onMerged();
    } catch (error) {
      onError((error as Error).message);
      setBusy(false);
    }
  };
  return (
    <Dialog title="Объединить размеры в одну карточку" onClose={onClose}>
      <p>
        Выберите основную карточку — его название, категория и описание останутся основными.
        Размеры, остатки, цены, фотографии и связи СБИС остальных черновиков переедут в него.
      </p>
      <label className="wide">
        Основная карточка
        <AdminProductSearch
          label="Основная карточка"
          value={String(targetId)}
          disabled={busy}
          options={products.map((item) => ({
            value: String(item.id),
            name: item.name,
            detail: item.sabyCode || item.sku,
            search: `${item.latinName} ${item.variantLabel} ${item.productCode} ${item.sku} ${item.slug} ${item.id}`,
          }))}
          onChange={(value) => setTargetId(Number(value))}
        />
      </label>
      <div className="admin-merge-preview">
        <strong>Будет одна карточка: {target?.name}</strong>
        <span>Вариантов после объединения: не менее {products.length}</span>
        <small>Поглощаемые черновики: {sources.map((item) => item.name).join(", ")}</small>
      </div>
      <p className="admin-hint">
        Объединяются только черновики без заказов и отзывов. Все SKU и коды СБИС сохраняются.
      </p>
      <div className="dialog-actions">
        <button onClick={onClose}>Отмена</button>
        <button className="primary" disabled={busy || sources.length === 0} onClick={merge}>
          Объединить {products.length} карточки
        </button>
      </div>
    </Dialog>
  );
}

type PublishedMergePreview = {
  ready: boolean;
  targetProductCode?: string;
  sourceProductCodes?: string[];
  skus: string[];
  blockers: string[];
};

export function PublishedMergePreviewDialog({
  products,
  onClose,
  onMerged,
  onError,
}: {
  products: Product[];
  onClose: () => void;
  onMerged: () => void;
  onError: (value: string) => void;
}) {
  const [editorialProducts, setEditorialProducts] = useState(products);
  const [contentNotice, setContentNotice] = useState("");
  const [targetId, setTargetId] = useState(products[0]?.id || 0);
  const [expectedSKUs, setExpectedSKUs] = useState("");
  const [mergeSizes, setMergeSizes] = useState<AdminVariant[]>([]);
  const [sizeError, setSizeError] = useState("");
  useEffect(() => {
    let active = true;
    Promise.all(
      products.map((product) =>
        api<{ variants: AdminVariant[] }>(`/api/v1/admin/products/${product.id}/variants`),
      ),
    )
      .then((rows) => {
        if (active) {
          const variants = rows.flatMap((x) => x.variants || []).filter((x) => !x.archived);
          setMergeSizes(variants);
          setExpectedSKUs(variants.map((x) => x.sku).join(", "));
        }
      })
      .catch((error) => {
        if (active) setSizeError(error.message);
      });
    return () => {
      active = false;
    };
  }, [products]);
  const [reviewedIdentity, setReviewedIdentity] = useState(products[0]?.name || "");
  const [preview, setPreview] = useState<PublishedMergePreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const contentTarget = editorialProducts.find((item) => item.id === targetId);
  const contentSources = contentTarget
    ? editorialProducts
        .filter((item) => item.id !== targetId)
        .map((item) => ({ item, patch: mergeContentPatch(contentTarget, item) }))
        .filter((item) => Object.keys(item.patch).length)
    : [];
  const reconcileContent = async () => {
    if (!canReconcileMergeContent(editorialProducts, contentTarget)) return;
    setBusy(true);
    setPreview(null);
    setConfirmation("");
    setContentNotice("");
    const updated = [...editorialProducts];
    try {
      for (const { item, patch } of contentSources) {
        const result = await api<{ product: Product }>(`/api/v1/admin/products/${item.id}`, {
          method: "PATCH",
          body: JSON.stringify(patch),
        });
        updated[updated.findIndex((row) => row.id === item.id)] = result.product;
      }
      setContentNotice(
        "Контент сохранён. Повторите проверку объединения; размеры и связи не перемещались.",
      );
    } catch (error) {
      onError((error as Error).message);
      setContentNotice(
        "Согласование не завершено. Сохранённые карточки показаны ниже; проверьте оставшиеся различия.",
      );
    } finally {
      setEditorialProducts(updated);
      setBusy(false);
    }
  };
  const requestBody = () => ({
    targetProductId: targetId,
    sourceProductIds: products.filter((item) => item.id !== targetId).map((item) => item.id),
    expectedSkus: expectedSKUs
      .split(/[\s,;]+/)
      .map((value) => value.trim())
      .filter(Boolean),
    reviewedIdentity: reviewedIdentity.trim(),
  });
  const check = async () => {
    setBusy(true);
    setPreview(null);
    setConfirmation("");
    try {
      const result = await api<PublishedMergePreview>(
        "/api/v1/admin/products/merge-published/preview",
        {
          method: "POST",
          body: JSON.stringify(requestBody()),
        },
      );
      setPreview(result);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const merge = async () => {
    if (!preview?.ready || confirmation !== `ОБЪЕДИНИТЬ ${preview.targetProductCode}`) return;
    setBusy(true);
    try {
      await api("/api/v1/admin/products/merge-published", {
        method: "POST",
        body: JSON.stringify(requestBody()),
      });
      onMerged();
    } catch (error) {
      setPreview(null);
      setConfirmation("");
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog title="Проверка и объединение опубликованных карточек" onClose={onClose}>
      <p>
        Проверка только читает базу. Выберите основную карточку. Все её размеры и размеры исходных
        карточек загружаются автоматически. После проверки товары останутся без изменений.
      </p>
      <ul>
        {products.map((item) => (
          <li key={item.id}>
            {item.name} · внутренний ID {item.id} · адрес {item.slug} · {item.sku} ·{" "}
            {item.latinName || "латинское название не заполнено"}
          </li>
        ))}
      </ul>
      <label className="wide">
        Основная карточка
        <AdminProductSearch
          label="Основная карточка"
          value={String(targetId)}
          disabled={busy}
          options={products.map((item) => ({
            value: String(item.id),
            name: item.name,
            detail: `ID ${item.id} · ${item.slug} · ${item.sku}`,
            search: `${item.latinName} ${item.variantLabel} ${item.productCode} ${item.sabyCode}`,
          }))}
          onChange={(value) => {
            setTargetId(Number(value));
            setReviewedIdentity(products.find((item) => item.id === Number(value))?.name || "");
            setPreview(null);
            setConfirmation("");
            setContentNotice("");
          }}
        />
      </label>
      <label className="wide">
        Подтверждённый вид или сорт
        <input
          value={reviewedIdentity}
          onChange={(event) => {
            setReviewedIdentity(event.target.value);
            setPreview(null);
          }}
        />
      </label>
      <section>
        <h3>Размеры, участвующие в проверке</h3>
        {sizeError ? (
          <p className="admin-inline-error" role="alert">
            {sizeError}
          </p>
        ) : !mergeSizes.length ? (
          <p>Загружаем размеры…</p>
        ) : (
          <ul>
            {mergeSizes.map((x) => (
              <li key={x.id}>
                {products.find((p) => p.id === x.productId)?.name} · {x.label} · артикул {x.sku}
              </li>
            ))}
          </ul>
        )}
      </section>
      {contentSources.length > 0 && (
        <section>
          <h3>Различия контента</h3>
          <p>
            Проверьте значения перед сохранением. В перечисленных полях исходных карточек будет
            использован контент основной. Пустые поля основной не стирают заполненные сведения.
          </p>
          {mergeContentFields
            .filter(([key]) => contentSources.some((source) => key in source.patch))
            .map(([key, label]) => (
              <details key={key}>
                <summary>{label}</summary>
                {[
                  contentTarget!,
                  ...contentSources
                    .filter((source) => key in source.patch)
                    .map((source) => source.item),
                ].map((item) => (
                  <div key={item.id}>
                    <strong>
                      {item.id === targetId ? "Основная" : "Исходная"}: {item.sku} · {item.name}
                    </strong>
                    <p style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
                      {typeof item[key] === "string"
                        ? String(item[key])
                        : JSON.stringify(item[key], null, 2)}
                    </p>
                  </div>
                ))}
              </details>
            ))}
          <button
            disabled={busy || !canReconcileMergeContent(editorialProducts, contentTarget)}
            onClick={() => void reconcileContent()}
          >
            Сохранить контент основной в исходных карточках
          </button>
        </section>
      )}
      {contentNotice && <p role="status">{contentNotice}</p>}
      {preview && (
        <div className="admin-merge-preview">
          <strong>
            {preview.ready ? "Предварительные проверки пройдены" : "Объединение заблокировано"}
          </strong>
          <span>
            Основная карточка: {preview.targetProductCode || "—"}; исходные:{" "}
            {preview.sourceProductCodes?.join(", ") || "—"}
          </span>
          {preview.blockers.map((blocker) => (
            <p className="admin-inline-error" key={blocker}>
              {blocker}
            </p>
          ))}
          {preview.ready && (
            <small>
              Результат может устареть. Перед фактическим объединением сервер повторит все проверки.
            </small>
          )}
        </div>
      )}
      {preview?.ready && (
        <label className="wide">
          Для объединения введите «ОБЪЕДИНИТЬ {preview.targetProductCode}»
          <input
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            autoComplete="off"
          />
        </label>
      )}
      <div className="dialog-actions">
        <button onClick={onClose}>Закрыть</button>
        <button
          disabled={busy || !!sizeError || !reviewedIdentity.trim() || !expectedSKUs.trim()}
          onClick={() => void check()}
        >
          {busy ? "Подождите…" : "Проверить без изменений"}
        </button>
        {preview?.ready && (
          <button
            className="admin-danger"
            disabled={busy || confirmation !== `ОБЪЕДИНИТЬ ${preview.targetProductCode}`}
            onClick={() => void merge()}
          >
            Объединить карточки
          </button>
        )}
      </div>
    </Dialog>
  );
}
