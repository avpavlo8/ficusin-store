import { useCallback, useEffect, useState } from "react";
import { api } from "./adminShared";
import { WorkspaceState, WorkspaceTable } from "./WorkspaceUI";
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
};
type State = { items: Item[]; configured: boolean; publicationMode: string; shopId: string };
type Remote = {
  productId: number;
  offerId: string;
  name: string;
  price: string;
  stock: number;
  type: number;
};
export default function AdminFlowwow() {
  const [data, setData] = useState<State | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");
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
          ["failed", "needs_attention"].includes(i.status)) &&
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
      {data && (
        <>
          <p role="status">
            {data.configured
              ? `API подключён · Магазин ${data.shopId}`
              : "API не подключён. Добавьте FLOWWOW_API_TOKEN и FLOWWOW_SHOP_ID в окружение сервера."}{" "}
            ·{" "}
            {data.publicationMode === "live"
              ? "Отправка включена"
              : data.publicationMode === "mock"
                ? "Локальная проверка. Реальный магазин не меняется."
                : "Отправка выключена. Карточки сохраняются для подготовки."}
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
              Карточки в Flowwow
            </button>
          </div>
          {items.length ? (
            <WorkspaceTable label="Связанные товары Flowwow">
              <thead>
                <tr>
                  <th>Товар на сайте</th>
                  <th>Карточка Flowwow</th>
                  <th>Цена сайта</th>
                  <th>Остаток сайта</th>
                  <th>Обмен</th>
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
                      <span role="status">{i.lastError || i.message}</span>
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
                  </tr>
                ))}
              </tbody>
            </WorkspaceTable>
          ) : (
            <WorkspaceState
              kind="empty"
              title={
                filter === "attention" ? "Ошибок обмена не найдено" : "Товаров Flowwow пока нет"
              }
              detail="Откройте товар в каталоге, перейдите в «Маркетплейсы» и настройте Flowwow."
              action={
                <a className="admin-action" href="/admin?section=products">
                  Открыть каталог
                </a>
              }
            />
          )}
          {remote && (
            <section>
              <h3>Карточки магазина Flowwow</h3>
              <p>
                Чтобы сохранить существующую карточку, выберите «Связать существующую» в редакторе
                товара и укажите её ID и артикул.
              </p>
              <WorkspaceTable label="Каталог Flowwow">
                <thead>
                  <tr>
                    <th>Название</th>
                    <th>ID</th>
                    <th>Артикул</th>
                    <th>Цена</th>
                    <th>Остаток</th>
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
