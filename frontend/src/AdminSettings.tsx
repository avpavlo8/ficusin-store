import { useEffect, useState } from "react";
import { PageHeading, api } from "./adminShared";
import type { Order, SettingDefinition } from "./adminTypes";

// The switches the owner flips instead of asking for a redeploy: turning an
// integration off for a test run, the sender details, how long an unpaid
// order waits.
export function Settings({ onError }: { onError: (value: string) => void }) {
  const [definitions, setDefinitions] = useState<SettingDefinition[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState("");

  useEffect(() => {
    api<{ definitions: SettingDefinition[]; values: Record<string, string> }>("/api/v1/admin/settings")
      .then((data) => { setDefinitions(data.definitions); setValues(data.values); })
      .catch((error) => onError((error as Error).message));
  }, [onError]);

  const save = async () => {
    try {
      const result = await api<{ values: Record<string, string> }>("/api/v1/admin/settings", { method: "PUT", body: JSON.stringify({ values }) });
      setValues(result.values);
      setSaved("Настройки сохранены и уже действуют");
      window.setTimeout(() => setSaved(""), 3000);
    } catch (error) { onError((error as Error).message); }
  };

  return <><PageHeading eyebrow="Магазин" title="Настройки" text="Действуют сразу, перезапуск не нужен" />
    <div className="admin-settings">
      {definitions.map((definition) => <label key={definition.key} className={definition.kind === "switch" ? "admin-setting switch" : "admin-setting"}>
        {definition.kind === "switch"
          ? <input type="checkbox" checked={values[definition.key] !== "0"} onChange={(event) => setValues({ ...values, [definition.key]: event.target.checked ? "1" : "0" })} />
          : null}
        <span>
          <b>{definition.title}</b>
          <small>{definition.note}</small>
        </span>
        {definition.kind !== "switch"
          ? <input type={definition.kind === "number" ? "number" : "text"} min="0" value={values[definition.key] ?? ""} onChange={(event) => setValues({ ...values, [definition.key]: event.target.value })} />
          : null}
      </label>)}
      <div className="admin-settings-actions">
        <button className="primary" onClick={save}>Сохранить</button>
        {saved && <span>{saved}</span>}
      </div>
    </div>
  </>;
}

// The manager names the delivery price for an order the shop could not
// quote. Kept as its own component so the field holds what is being typed
// without re-rendering the whole order table on every keystroke.
export function DeliveryFeeForm({ order, onSubmit }: { order: Order; onSubmit: (order: Order, fee: number) => void }) {
  const [value, setValue] = useState("");
  return <form className="admin-fee-form" onSubmit={(event) => { event.preventDefault(); const fee = Number(value); if (Number.isFinite(fee) && fee >= 0) onSubmit(order, fee); }}>
    <strong>{order.repackRequested ? "Покупатель просит упаковать в одну коробку" : "Доставку нужно посчитать вручную"}</strong>
    <p>Укажите стоимость доставки — покупатель получит уведомление и сможет оплатить заказ.</p>
    <label>Доставка, ₽<input type="number" min="0" step="1" value={value} onChange={(event) => setValue(event.target.value)} placeholder="0" required /></label>
    <button className="primary" type="submit">Сохранить и уведомить</button>
  </form>;
}
