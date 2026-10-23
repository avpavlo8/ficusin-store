import { ReturnNumberInput } from "./ReturnNumberInput";
import { ReturnReceiptBadge, ReturnStateIcon } from "./ReturnState";
import { useEffect, useRef, useState } from "react";
import { api, Dialog } from "./adminShared";
import { downloadReturnLabels, returnMoney, returnPhotoBase64 } from "./returnHelpers";
import type { MarketplaceReturn } from "./adminTypes";
import { useReturnAssessment } from "./useReturnAssessment";
import { discountPrice, returnChannels, returnConditions, returnDate } from "./returnWorkspace";

export function ReturnDetails({
  item,
  canEdit,
  canReceipt,
  canFinance = false,
  onSaved,
  onSell,
  onDocument,
  onBack,
  registerFlush,
}: {
  item: MarketplaceReturn;
  canFinance?: boolean;
  canEdit: boolean;
  canReceipt: boolean;
  onSaved: (item: MarketplaceReturn) => void;
  onSell: (id: number) => Promise<void>;
  onDocument: (id: string) => void;
  onBack: () => void;
  registerFlush: (flush: () => Promise<void>) => () => void;
}) {
  const { draft, update, flush, state, error } = useReturnAssessment(item, canEdit, onSaved);
  const [busy, setBusy] = useState(false),
    [actionError, setActionError] = useState(""),
    [progress, setProgress] = useState("");
  const [preview, setPreview] = useState<number>(),
    [removedPhoto, setRemovedPhoto] = useState<number>();
  const upload = useRef<HTMLInputElement>(null),
    replacement = useRef<number | undefined>(undefined);
  useEffect(() => registerFlush(flush), [flush, registerFlush]);
  const validDiscount =
    Number.isInteger(draft.discountPercent) &&
    draft.discountPercent >= 0 &&
    draft.discountPercent <= 100;
  const price = validDiscount ? discountPrice(item.price, draft.discountPercent) : undefined;
  const run = async (action: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setActionError("");
    try {
      await flush();
      await action();
    } catch (cause) {
      setActionError(
        cause instanceof Error
          ? cause.message
          : "Не удалось выполнить действие. Попробуйте ещё раз.",
      );
    } finally {
      setBusy(false);
      setProgress("");
    }
  };
  const receipt = () =>
    run(async () => {
      const result = await api<{ item: MarketplaceReturn }>(
        `/api/v1/admin/returns/${item.id}/receipt`,
        { method: "POST" },
      );
      onSaved(result.item);
    });
  const photo = (file?: File) => {
    if (!file) return;
    const replacePhotoId = replacement.current;
    replacement.current = undefined;
    void run(async () => {
      setProgress("Подготавливаем фото…");
      const data = await returnPhotoBase64(file);
      setProgress("Загружаем фото…");
      const result = await api<{ item: MarketplaceReturn }>(
        `/api/v1/admin/returns/${item.id}/photos`,
        {
          method: "POST",
          body: JSON.stringify({ contentType: "image/jpeg", data, replacePhotoId }),
        },
      );
      onSaved(result.item);
      setRemovedPhoto(undefined);
    });
  };
  const removePhoto = (id: number) =>
    run(async () => {
      const result = await api<{ item: MarketplaceReturn }>(
        `/api/v1/admin/returns/${item.id}/photos/${id}`,
        { method: "DELETE" },
      );
      onSaved(result.item);
      setRemovedPhoto(id);
    });
  const restorePhoto = () =>
    run(async () => {
      if (!removedPhoto) return;
      const result = await api<{ item: MarketplaceReturn }>(
        `/api/v1/admin/returns/${item.id}/photos/${removedPhoto}/restore`,
        { method: "POST" },
      );
      onSaved(result.item);
      setRemovedPhoto(undefined);
    });
  const saving = state === "saving" || busy;
  const published =
    validDiscount &&
    draft.condition === "restoring" &&
    draft.discountPercent > 0 &&
    (item.price || 0) > 0;
  return (
    <aside className="return-detail" aria-label={`Экземпляр ${item.article}`}>
      <div className="return-detail-toolbar">
        <button type="button" className="return-back" onClick={onBack}>
          ← К списку растений
        </button>
        <span className={`return-autosave ${state}`} role="status">
          {state === "saving"
            ? "Сохраняется…"
            : state === "dirty"
              ? "Есть изменения…"
              : state === "error"
                ? "Не сохранено"
                : canEdit
                  ? "Оценка сохранена"
                  : "Только просмотр"}
        </span>
      </div>
      <header>
        <div>
          <p className="return-eyebrow">Экземпляр {item.article}</p>
          <h2>{item.productName}</h2>
          <div className="return-detail-meta">
            {item.label && <span className="return-size">{item.label}</span>}
            <span>
              {returnChannels[item.channel]} · {returnDate(item.returnedAt)}
            </span>
          </div>
        </div>
      </header>
      <div className="return-price">
        <strong>{price == null ? "—" : returnMoney(price)}</strong>
        {validDiscount && draft.discountPercent > 0 && (
          <>
            <del>{returnMoney(item.price)}</del>
            <span>−{draft.discountPercent}%</span>
          </>
        )}
        <small>
          Цена этого экземпляра{state !== "clean" ? " · есть несохранённые изменения" : ""}
        </small>
      </div>
      <section>
        <h3>Состояние растения</h3>
        <div className="return-condition-grid">
          {Object.entries(returnConditions).map(([condition, meta]) => (
            <button
              type="button"
              key={condition}
              aria-pressed={draft.condition === condition}
              disabled={!canEdit || busy}
              className={`${meta.className} ${draft.condition === condition ? "active" : ""}`}
              onClick={() => update({ condition: condition as MarketplaceReturn["condition"] })}
            >
              <ReturnStateIcon condition={condition as MarketplaceReturn["condition"]} />
              <span>
                <strong>{meta.label}</strong>
                <small>{meta.note}</small>
              </span>
            </button>
          ))}
        </div>
        <p className="return-field-note">
          Состояние, скидка и комментарии сохраняются автоматически.
        </p>
        {draft.condition !== item.condition && item.receiptStatus !== "none" && (
          <p className="return-notice">
            Для растения уже есть операция СБИС. После смены состояния проверьте состав связанного
            поступления.
          </p>
        )}
      </section>
      {(draft.condition === "restoring" ||
        (draft.discountPercent > 0 && draft.condition === "ready")) && (
        <section className="return-discount-section">
          <label className="return-comment">
            <span>Скидка для этого экземпляра, %</span>
            <ReturnNumberInput
              aria-label="Скидка экземпляра"
              aria-invalid={draft.discountPercent < 0 || draft.discountPercent > 100}
              value={draft.discountPercent}
              disabled={!canEdit || busy}
              onFocus={(event) => event.target.select()}
              onValue={(discountPercent) => update({ discountPercent })}
            />
          </label>
          <p className="return-field-note">
            При продаже менеджер указывает эту скидку в кассе вручную.
          </p>
          {draft.condition === "restoring" && canReceipt && canEdit && (
            <div className="return-next-step">
              <strong>Готовы продать этот экземпляр?</strong>
              <p>Создайте поступление, проведите его в СБИС, затем продайте растение в кассе.</p>
              {item.receiptStatus === "none" ? (
                <button
                  type="button"
                  className="returns-action"
                  disabled={busy}
                  onClick={() => void run(() => onSell(item.id))}
                >
                  Подготовить к продаже
                </button>
              ) : (
                <p className="return-notice">
                  Операция уже существует. Переведите растение в продажу, чтобы открыть и проверить
                  её.
                </p>
              )}
            </div>
          )}
        </section>
      )}
      <section>
        <div className="return-section-heading">
          <h3>Фото осмотра</h3>
          <span>{(item.photoIds || []).length} / 6</span>
        </div>
        <div className="return-photos">
          {(item.photoIds || []).map((id, index) => (
            <div className="return-photo" key={id}>
              <button
                type="button"
                className="return-photo-preview"
                aria-label={`Увеличить фото ${index + 1}`}
                onClick={() => setPreview(id)}
              >
                <img src={`/api/v1/admin/return-photos/${id}`} alt={`Фото осмотра ${index + 1}`} />
              </button>
              {canEdit && (
                <div className="return-photo-actions">
                  <button
                    type="button"
                    disabled={busy}
                    aria-label={`Заменить фото ${index + 1}`}
                    onClick={() => {
                      replacement.current = id;
                      upload.current?.click();
                    }}
                  >
                    Заменить
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    aria-label={`Удалить фото ${index + 1}`}
                    onClick={() => void removePhoto(id)}
                  >
                    Удалить
                  </button>
                </div>
              )}
            </div>
          ))}
          {canEdit && (item.photoIds || []).length < 6 && (
            <button
              type="button"
              className="return-add-photo"
              disabled={busy}
              onClick={() => {
                replacement.current = undefined;
                upload.current?.click();
              }}
            >
              +<span>Добавить фото</span>
            </button>
          )}
        </div>
        <input
          ref={upload}
          tabIndex={-1}
          className="return-file-input"
          type="file"
          accept="image/jpeg,image/png,image/webp"
          aria-label="Загрузить фото осмотра"
          disabled={busy}
          onChange={(event) => {
            photo(event.target.files?.[0]);
            event.target.value = "";
          }}
        />
        <p className="return-field-note">
          JPEG, PNG или WebP. Фото уменьшается перед загрузкой. Нажмите на снимок, чтобы увеличить.
        </p>
        {progress && <p role="status">{progress}</p>}
        {removedPhoto && (
          <p className="return-photo-undo">
            Фото удалено.{" "}
            <button type="button" disabled={busy} onClick={() => void restorePhoto()}>
              Восстановить фото
            </button>
          </p>
        )}
      </section>
      <section>
        <label className="return-comment">
          <span>Комментарий осмотра · для команды</span>
          <textarea
            aria-label="Комментарий осмотра"
            value={draft.comment}
            maxLength={4000}
            disabled={!canEdit || busy}
            placeholder="Что произошло с растением и какой уход нужен"
            onChange={(event) => update({ comment: event.target.value })}
          />
        </label>
      </section>
      {draft.condition === "restoring" && (
        <section className="return-publication">
          <div className="return-section-heading">
            <h3>На сайте со скидкой</h3>
            <span className="return-publication-state">
              {published ? "Опубликован" : "Не опубликован"}
            </span>
          </div>
          <p>
            {published
              ? "После сохранения покупатель увидит цену этого экземпляра и описание ниже."
              : "Экземпляр появится в разделе скидок, когда вы назначите скидку больше 0%."}
          </p>
          <label className="return-comment">
            <span>Состояние для покупателя</span>
            <textarea
              aria-label="Описание для покупателя"
              value={draft.publicComment}
              maxLength={1000}
              disabled={!canEdit || busy}
              placeholder="Например: повреждён один лист, новый рост здоровый"
              onChange={(event) => update({ publicComment: event.target.value })}
            />
          </label>
          <p className="return-field-note">
            {item.photoIds?.length
              ? "На витрине будет первое фото осмотра."
              : "Сейчас на витрине используется фото из каталога с предупреждением."}{" "}
            Внутренний комментарий не публикуется.
          </p>
          {published && (
            <a
              href={`/discounts#${item.article}`}
              target="_blank"
              rel="noreferrer"
              onClick={(event) => {
                event.preventDefault();
                const preview = window.open("about:blank", "_blank");
                if (preview) preview.opener = null;
                void flush()
                  .then(() => {
                    if (preview) preview.location.href = `/discounts#${item.article}`;
                  })
                  .catch(() => preview?.close());
              }}
            >
              Предпросмотр экземпляра ↗
            </a>
          )}
        </section>
      )}
      {draft.condition === "ready" && (
        <section className="return-receipt">
          <div className="return-section-heading">
            <h3>Поступление в СБИС</h3>
            <ReturnReceiptBadge item={item} />
          </div>
          {item.receiptExternalUrl ? (
            <>
              <p>
                {item.receiptStatus === "posted"
                  ? "Поступление проведено."
                  : "Документ создан. Проведите его в СБИС, затем продавайте растение в кассе."}
              </p>
              {item.receiptStatus === "correction_required" && (
                <p className="return-notice">
                  Оценка растения менялась. Проверьте состав документа; новое поступление создавать
                  не нужно.
                </p>
              )}
              <a
                className="returns-action"
                href={item.receiptExternalUrl}
                target="_blank"
                rel="noreferrer"
              >
                Открыть поступление ↗
              </a>
            </>
          ) : (
            <>
              <p>
                {item.receiptStatus === "none"
                  ? "Следующий шаг — создать поступление для этого экземпляра."
                  : ["queued", "checking"].includes(item.receiptStatus)
                    ? "Операция выполняется. Повторный документ не создаётся."
                    : "Проверьте существующую операцию перед повторным созданием документа."}
              </p>
              {canReceipt && item.receiptStatus === "none" && (
                <button
                  type="button"
                  className="returns-action"
                  disabled={busy}
                  onClick={() => void receipt()}
                >
                  Создать поступление
                </button>
              )}
              {item.receiptStatus !== "none" && (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const result = await api<{ item: MarketplaceReturn }>(
                        `/api/v1/admin/returns/${item.id}`,
                      );
                      onSaved(result.item);
                    })
                  }
                >
                  Проверить операцию
                </button>
              )}
            </>
          )}
        </section>
      )}
      <details className="return-accounting">
        <summary>Учёт и документ приёмки</summary>
        <dl>
          <div>
            <dt>Себестоимость на сайте</dt>
            <dd>{item.unitCost == null ? "Неизвестна" : returnMoney(item.unitCost)}</dd>
          </div>
          <div>
            <dt>Связь с продажей</dt>
            <dd>{item.financialStatus === "linked" ? "Продажа связана" : "Принято вручную"}</dd>
          </div>
        </dl>
        {item.financialStatus !== "linked" && (
          <p>
            Возврат принят без привязки к исходной продаже.{" "}
            {canFinance ? (
              <>
                Для проверки финансовых показателей перейдите в{" "}
                <a href="/admin?section=finance">«Финансы»</a>.
              </>
            ) : (
              "Для проверки финансовых показателей передайте владельцу RET-артикул экземпляра. Раздел финансов доступен владельцу."
            )}
          </p>
        )}
        {item.channelComment && <p>{item.channelComment}</p>}
        {item.intakeId && (
          <button type="button" onClick={() => onDocument(item.intakeId!)}>
            Открыть документ приёмки
          </button>
        )}
      </details>
      {(item.history || []).length > 0 && (
        <details className="return-history">
          <summary>История оценки · {item.history.length}</summary>
          {item.history.map((entry, index) => (
            <p key={`${entry.createdAt}-${index}`}>
              <time>{new Date(entry.createdAt).toLocaleString("ru-RU")}</time>
              <strong>
                {returnConditions[entry.to as keyof typeof returnConditions]?.label || entry.to}
              </strong>
              {entry.comment && <span>{entry.comment}</span>}
            </p>
          ))}
        </details>
      )}
      <div className="return-detail-footer">
        {(error || actionError) && (
          <p role="alert" className="return-inline-error">
            {error || actionError}
          </p>
        )}
        <span role="status">
          {state === "clean"
            ? "Все изменения сохранены"
            : state === "error"
              ? "Изменения остаются в этой карточке"
              : "Сохраняем изменения автоматически"}
        </span>
        {canEdit && (
          <button
            type="button"
            className="return-save"
            disabled={saving || state === "clean"}
            onClick={() => void flush().catch(() => {})}
          >
            {state === "error" ? "Повторить сохранение" : "Сохранить сейчас"}
          </button>
        )}
        {draft.condition === "restoring" && (
          <button
            type="button"
            disabled={busy}
            onClick={() => void run(() => downloadReturnLabels([item.id]))}
          >
            Скачать этикетку
          </button>
        )}
      </div>
      {preview != null && (
        <Dialog
          title="Фото осмотра"
          className="return-photo-dialog"
          onClose={() => setPreview(undefined)}
        >
          <img
            src={`/api/v1/admin/return-photos/${preview}`}
            alt="Увеличенное фото возвращённого растения"
          />
        </Dialog>
      )}
    </aside>
  );
}
