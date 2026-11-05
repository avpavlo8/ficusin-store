import { useEffect, useState } from "react";
import { api, money } from "./adminShared";
import SabyLinkPicker from "./SabyLink";
import type { AdminVariant } from "./AdminPim";

type ExportView = {
  configured: boolean;
  simulation?: boolean;
  state: "idle" | "queued" | "checking" | "verifying" | "unknown" | "synced" | "error";
  message: string;
  fingerprint: string;
  dirty: boolean;
  validation: string;
  verifiedAt?: string;
  snapshot: {
    productId: number;
    sku: string;
    name: string;
    potCm: number;
    heightCm: number;
    priceMinor: number;
    sabyId: string;
  };
};
const pending = (state: string) => ["queued", "checking", "verifying", "unknown"].includes(state);
const labels: Record<ExportView["state"], string> = {
  idle: "Ещё не отправлено",
  queued: "Ожидает отправки",
  checking: "Сверяем карточку",
  verifying: "Проверяем результат",
  unknown: "Нужна сверка результата",
  synced: "Подтверждено в СБИС",
  error: "Не удалось отправить",
};

export default function SabyExport({
  variant,
  dirty,
  onReload,
}: {
  variant: AdminVariant;
  dirty: boolean;
  onReload: () => void;
}) {
  const [view, setView] = useState<ExportView | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const endpoint = `/api/v1/admin/variants/${variant.id}/saby-export`;
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const read = async () => {
      try {
        const next = await api<ExportView>(endpoint);
        if (!active) return;
        if (!next.snapshot || !(next.state in labels))
          throw new Error("Не удалось прочитать состояние отправки в СБИС");
        setView(next);
        setError("");
        if (pending(next.state)) timer = setTimeout(() => void read(), 5000);
      } catch (cause) {
        if (active) setError((cause as Error).message);
      }
    };
    void read();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [endpoint, dirty, revision]);
  const send = async (checkOnly = false) => {
    if (!view || busy || (dirty && !checkOnly)) return;
    setBusy(true);
    setError("");
    try {
      const next = await api<ExportView>(endpoint, {
        method: "POST",
        body: JSON.stringify({ fingerprint: view.fingerprint, checkOnly }),
      });
      setView(next);
      setRevision((value) => value + 1);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const linked = Boolean(view?.snapshot.sabyId);
  const waiting = view && pending(view.state);
  return (
    <section className="catalog-saby-export" aria-label={`СБИС: артикул ${variant.sku}`}>
      <div className="catalog-saby-heading">
        <h4>СБИС</h4>
        <span aria-live="polite">
          {view
            ? view.simulation && view.state === "synced"
              ? "Тестовый прогон завершён"
              : labels[view.state]
            : "Загружаем состояние…"}
        </span>
      </div>
      {view && (
        <>
          <p>
            <strong>{view.snapshot.name}</strong>
          </p>
          {view.simulation && (
            <p className="admin-hint">
              <strong>Тестовый контур.</strong> Проверяем создание и сохранение связи здесь. В
              настоящий СБИС карточки и цены не отправляются.
            </p>
          )}
          <dl className="catalog-saby-values">
            <div>
              <dt>Горшок</dt>
              <dd>{view.snapshot.potCm || "—"} см</dd>
            </div>
            <div>
              <dt>Высота</dt>
              <dd>{view.snapshot.heightCm || "—"} см</dd>
            </div>
            <div>
              <dt>Цена</dt>
              <dd>{money.format(view.snapshot.priceMinor / 100)}</dd>
            </div>
          </dl>
          {dirty ? (
            <p className="admin-hint">
              Сначала сохраните правки карточки. Выше показаны данные последнего сохранения.
            </p>
          ) : view.validation ? (
            <p className="admin-inline-error">{view.validation}</p>
          ) : (
            <p className="admin-hint">
              {view.dirty
                ? "Отправим сохранённое название, горшок, высоту и основную цену. Остаток появится после складского обмена."
                : "Сохранённые данные уже подтверждены. Повторная отправка не нужна."}
            </p>
          )}
          {view.message && (
            <p
              className={
                view.state === "error" || view.state === "unknown"
                  ? "admin-inline-error"
                  : "admin-hint"
              }
            >
              {view.message}
            </p>
          )}
          {!view.configured && (
            <p className="admin-inline-error">
              Подключение СБИС не настроено. Данные карточки сохранены на сайте.
            </p>
          )}
          <div className="catalog-saby-actions">
            <button
              type="button"
              className="admin-primary"
              disabled={
                busy ||
                dirty ||
                !view.configured ||
                Boolean(view.validation) ||
                Boolean(waiting) ||
                !view.dirty
              }
              onClick={() => void send()}
            >
              {busy
                ? "Передаём…"
                : view.simulation
                  ? linked
                    ? "Обновить в СБИС (тест)"
                    : "Создать в СБИС (тест)"
                  : linked
                    ? "Обновить в СБИС"
                    : "Создать в СБИС"}
            </button>
            {waiting && (
              <button
                type="button"
                disabled={busy || !view.configured}
                onClick={() => void send(true)}
              >
                Проверить результат
              </button>
            )}
            {linked && <small>Связана с карточкой СБИС №{view.snapshot.sabyId}</small>}
          </div>
          {!linked && !dirty && !waiting && (
            <SabyLinkPicker
              label="Выбрать существующую карточку СБИС"
              product={{ id: view.snapshot.productId, name: view.snapshot.name }}
              variant={{ id: variant.id, sku: variant.sku, label: variant.label, sabyId: "" }}
              onLinked={() => {
                onReload();
                setRevision((value) => value + 1);
              }}
              onError={setError}
            />
          )}
        </>
      )}
      {error && (
        <p role="alert" className="admin-inline-error">
          {error}{" "}
          <button type="button" onClick={() => setRevision((value) => value + 1)}>
            Обновить состояние
          </button>
        </p>
      )}
    </section>
  );
}
