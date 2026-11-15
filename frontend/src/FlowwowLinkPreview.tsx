import { WorkspaceState, WorkspaceTable } from "./WorkspaceUI";

export type FlowwowPreviewItem = {
  productId?: number;
  name: string;
  sku: string;
  remoteId: string;
  offerId: string;
  priceMinor: number;
  stock: number;
  status: string;
  message: string;
};
export type FlowwowPreview = {
  shopId: string;
  capturedAt: string;
  items: FlowwowPreviewItem[];
};

export function FlowwowLinkPreview({
  preview,
  items,
}: {
  preview: FlowwowPreview;
  items: FlowwowPreviewItem[];
}) {
  const captured = new Date(preview.capturedAt).toLocaleString("ru-RU", {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "Europe/Moscow",
  });
  return (
    <div role="region" aria-label="Снимок рабочего магазина Flowwow">
      <h3>Реальные связки · {preview.items.length}</h3>
      <p>
        Снимок рабочего магазина {preview.shopId} от{" "}
        <time dateTime={preview.capturedAt}>{captured} МСК</time>. Только просмотр.
      </p>
      {items.length ? (
        <WorkspaceTable label="Реальные связки Flowwow">
          <thead>
            <tr>
              <th>Товар</th>
              <th>Карточка Flowwow</th>
              <th>Цена рабочего сайта</th>
              <th>Остаток рабочего сайта</th>
              <th>Обмен на рабочем сайте</th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.sku}>
                <td>
                  {item.productId ? (
                    <a href={`/admin?section=products&product=${item.productId}&editor=channels`}>
                      {item.name}
                    </a>
                  ) : (
                    item.name
                  )}
                  <small>Артикул сайта: {item.sku}</small>
                </td>
                <td>
                  <a
                    href={`https://flowwow.com/admin/products/${item.remoteId}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    ID {item.remoteId}
                  </a>
                  <small>Артикул Flowwow: {item.offerId}</small>
                </td>
                <td>{(item.priceMinor / 100).toLocaleString("ru-RU")} ₽</td>
                <td>{item.stock} шт.</td>
                <td>{item.message}</td>
              </tr>
            ))}
          </tbody>
        </WorkspaceTable>
      ) : (
        <WorkspaceState
          kind="empty"
          title={preview.items.length ? "Связи по запросу не найдены" : "В снимке пока нет связей"}
        />
      )}
    </div>
  );
}
