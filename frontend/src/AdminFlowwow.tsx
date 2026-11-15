import { useCallback, useEffect, useState } from "react";
import { api } from "./adminShared";
import { WorkspaceState, WorkspaceTable } from "./WorkspaceUI";
import { FlowwowBinding, type FlowwowRemote } from "./FlowwowBinding";
import { FlowwowLinkPreview, type FlowwowPreview } from "./FlowwowLinkPreview";
type Item = {
  variantId: number;
  productId: number;
  name: string;
  sku: string;
  priceMinor: number;
  stock: number;
  remoteId: string;
  offerId: string;
  status: string;
  message: string;
  lastError: string;
  contentUpdatedAt?: string;
  inventorySyncedAt?: string;
  inventoryPriceMinor?: number;
  inventoryStock?: number;
};
type State = {
  items: Item[];
  configured: boolean;
  publicationMode: string;
  shopId: string;
  preview?: FlowwowPreview | null;
};
type Remote = FlowwowRemote;
export default function AdminFlowwow() {
  const [data, setData] = useState<State | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");
  const [binding, setBinding] = useState<Remote | null>(null);
  const [notice, setNotice] = useState("");
  const [remote, setRemote] = useState<Remote[] | null>(null);
  const [page, setPage] = useState(0);
  const [total, setTotal] = useState(0);
  const load = useCallback(async () => {
    setBusy(true);
    setError("");
    try {
      setData(await api<State>("/api/v1/admin/flowwow"));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }, []);
  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 15000);
    return () => clearInterval(timer);
  }, [load]);
  const browse = async (next: number) => {
    setBusy(true);
    setError("");
    try {
      const result = await api<{ items: Remote[]; total: number }>(
        `/api/v1/admin/flowwow/products?page=${next}`,
      );
      setRemote(result.items || []);
      setTotal(result.total);
      setPage(next);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const items =
    data?.items.filter(
      (i) =>
        (filter !== "attention" ||
          i.lastError ||
          ["failed", "needs_attention"].includes(i.status) ||
          Boolean(i.lastError)) &&
        `${i.name} ${i.sku} ${i.offerId} ${i.remoteId}`
          .toLowerCase()
          .includes(search.toLowerCase()),
    ) || [];
  const previewItems =
    data?.preview?.items.filter(
      (i) =>
        (filter !== "attention" || ["failed", "needs_attention"].includes(i.status)) &&
        `${i.name} ${i.sku} ${i.offerId} ${i.remoteId}`
          .toLowerCase()
          .includes(search.toLowerCase()),
    ) || [];
  return (
    <section className="workspace-panel" aria-label="Flowwow">
      <div className="admin-section-head">
        <div>
          <h2>Flowwow</h2>
          <p>
            Каталог сайта — источник названий, описаний, фото и цен. Связи настраиваются в редакторе
            товара.
          </p>
        </div>
        <button className="admin-action" disabled={busy} onClick={() => void load()}>
          Обновить
        </button>
      </div>
      {error && (
        <WorkspaceState
          kind="error"
          title="Не удалось загрузить Flowwow"
          detail={error}
          action={
            <button className="admin-action" disabled={busy} onClick={() => void load()}>
              Повторить
            </button>
          }
        />
      )}
      {!data && !error && <WorkspaceState kind="loading" title="Загружаем состояние Flowwow…" />}
      {notice && <p role="status">{notice}</p>}
      {data && (
        <>
          <p role="status">
            {data.preview ? (
              "СТГ · Локальная проверка. Реальный магазин не меняется."
            ) : (
              <>
                {data.configured
                  ? `API подключён · Магазин ${data.shopId}`
                  : "API не подключён. Добавьте FLOWWOW_API_TOKEN и FLOWWOW_SHOP_ID в окружение сервера."}{" "}
                ·{" "}
                {data.publicationMode === "live"
                  ? "Отправка включена"
                  : data.publicationMode === "mock"
                    ? "Локальная проверка. Реальный магазин не меняется."
                    : "Отправка выключена. Карточки сохраняются для подготовки."}
              </>
            )}
          </p>
          <div className="admin-toolbar">
            <label>
              Найти товар
              <input
                type="search"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Название, артикул или ID"
              />
            </label>
            <label>
              Показать
              <select value={filter} onChange={(e) => setFilter(e.target.value)}>
                <option value="all">Все товары</option>
                <option value="attention">Требуют внимания</option>
              </select>
            </label>
            <button
              className="admin-action"
              disabled={busy || !data.configured}
              onClick={() => void browse(0)}
            >
              {data.preview ? "Тестовые карточки" : "Карточки в Flowwow"}
            </button>
          </div>
          {data.preview && <FlowwowLinkPreview preview={data.preview} items={previewItems} />}
          {items.length ? (
            <>
              {data.preview && <h3>Тестовые связки СТГ</h3>}
              <WorkspaceTable
                label={data.preview ? "Тестовые связки СТГ" : "Связанные товары Flowwow"}
              >
                <thead>
                  <tr>
                    <th>Товар на сайте</th>
                    <th>Карточка Flowwow</th>
                    <th>Цена сайта</th>
                    <th>Остаток сайта</th>
                    <th>Контент</th>
                    <th>Цена и остаток</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((i) => (
                    <tr key={i.variantId}>
                      <td>
                        <a href={`/admin?section=products&product=${i.productId}&editor=channels`}>
                          {i.name}
                        </a>
                        <small>{i.sku}</small>
                      </td>
                      <td>
                        {i.remoteId ? `ID ${i.remoteId}` : "Ещё не связана"}
                        <small>{i.offerId}</small>
                      </td>
                      <td>{(i.priceMinor / 100).toLocaleString("ru-RU")} ₽</td>
                      <td>{i.stock} шт.</td>
                      <td>
                        <span role="status">{i.message}</span>
                        {i.contentUpdatedAt && (
                          <small>
                            Обновлено:{" "}
                            {new Date(i.contentUpdatedAt).toLocaleString("ru-RU", {
                              timeZone: "Europe/Moscow",
                            })}
                          </small>
                        )}
                        {["failed", "needs_attention"].includes(i.status) && (
                          <button
                            type="button"
                            className="admin-action"
                            disabled={busy}
                            onClick={async () => {
                              setBusy(true);
                              try {
                                await api(
                                  `/api/v1/admin/products/${i.productId}/editor-publications/flowwow/retry`,
                                  {
                                    method: "POST",
                                    body: JSON.stringify({ variantId: i.variantId }),
                                  },
                                );
                                await load();
                              } catch (e) {
                                setError((e as Error).message);
                              } finally {
                                setBusy(false);
                              }
                            }}
                          >
                            Повторить проверку
                          </button>
                        )}
                      </td>
                      <td>
                        <span role="status">
                          {i.lastError || (i.inventorySyncedAt ? "Обновлены" : "Ожидают отправки")}
                        </span>
                        {i.inventorySyncedAt && (
                          <small>
                            {new Date(i.inventorySyncedAt).toLocaleString("ru-RU", {
                              timeZone: "Europe/Moscow",
                            })}{" "}
                            · {((i.inventoryPriceMinor || 0) / 100).toLocaleString("ru-RU")} ₽ ·{" "}
                            {i.inventoryStock} шт.
                          </small>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </WorkspaceTable>
            </>
          ) : !data.preview && !remote && !binding ? (
            <WorkspaceState
              kind="empty"
              title={
                filter === "attention"
                  ? "Ошибок обмена не найдено"
                  : "Связей с товарами сайта пока нет"
              }
              detail="Откройте товар в каталоге, перейдите в «Маркетплейсы» и настройте Flowwow."
              action={
                <a className="admin-action" href="/admin?section=products">
                  Открыть каталог
                </a>
              }
            />
          ) : null}
          {binding && (
            <FlowwowBinding
              remote={binding}
              onCancel={() => setBinding(null)}
              onSaved={() => {
                setBinding(null);
                setNotice("Связь подтверждена. Автоматический обмен поставлен в очередь.");
                void load();
              }}
            />
          )}
          {remote && (
            <section>
              <h3>Карточки магазина Flowwow</h3>
              <p>
                Выберите карточку и свяжите её с конкретным размером товара сайта. Новую карточку
                можно создать из редактора товара, включив «Отправлять в Flowwow».
              </p>
              <WorkspaceTable label="Каталог Flowwow">
                <thead>
                  <tr>
                    <th>Название</th>
                    <th>ID</th>
                    <th>Артикул</th>
                    <th>Цена</th>
                    <th>Остаток</th>
                    <th>Связь с сайтом</th>
                  </tr>
                </thead>
                <tbody>
                  {remote.map((i) => (
                    <tr key={i.productId}>
                      <td>
                        {i.name}
                        {i.type !== 1 && <small>Этот тип товара не поддерживает обмен</small>}
                      </td>
                      <td>{i.productId}</td>
                      <td>{i.offerId || "Не назначен"}</td>
                      <td>{i.price} ₽</td>
                      <td>{i.stock}</td>
                      <td>
                        {data.items.some((link) => link.remoteId === String(i.productId)) ? (
                          "Связана"
                        ) : (
                          <button
                            type="button"
                            className="admin-action"
                            disabled={busy || i.type !== 1}
                            onClick={() => {
                              setBinding(i);
                              setNotice("");
                            }}
                          >
                            Связать с товаром
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </WorkspaceTable>
              <div className="admin-toolbar">
                <button
                  className="admin-action"
                  disabled={busy || page === 0}
                  onClick={() => void browse(page - 1)}
                >
                  Назад
                </button>
                <span>
                  Страница {page + 1} · Всего {total}
                </span>
                <button
                  className="admin-action"
                  disabled={busy || (page + 1) * 50 >= total}
                  onClick={() => void browse(page + 1)}
                >
                  Далее
                </button>
              </div>
            </section>
          )}
        </>
      )}
    </section>
  );
}
