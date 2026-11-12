import "./styles/admin-settings-switches.css";
import { NumericInput } from "./NumericInput";
import { useCallback, useEffect, useState } from "react";
import { PageHeading, api } from "./adminShared";
import type { Order, SettingDefinition, ProcurementData, ProcurementSettings } from "./adminTypes";

import { ProcurementSettingsPanel } from "./AdminProcurementPanels";

// The switches the owner flips instead of asking for a redeploy: turning an
// integration off for a test run, the sender details, how long an unpaid
// order waits.
export function Settings({ onError }: { onError: (value: string) => void }) {
  const [tab, setTab] = useState(() =>
    new URLSearchParams(window.location.search).get("settingsTab") === "marketplaces"
      ? "marketplaces"
      : "site",
  );
  const [pricing, setPricing] = useState<ProcurementSettings | null>(null);
  const [pricingError, setPricingError] = useState("");
  const loadPricing = useCallback(
    () =>
      api<ProcurementData>("/api/v1/admin/procurement")
        .then((data) => {
          setPricing(data.settings);
          setPricingError("");
        })
        .catch((error: Error) => setPricingError(error.message)),
    [],
  );
  useEffect(() => {
    if (tab === "marketplaces" && !pricing) void loadPricing();
  }, [tab, pricing, loadPricing]);
  const [definitions, setDefinitions] = useState<SettingDefinition[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState("");

  useEffect(() => {
    api<{ definitions: SettingDefinition[]; values: Record<string, string> }>(
      "/api/v1/admin/settings",
    )
      .then((data) => {
        setDefinitions(data.definitions);
        setValues(data.values);
      })
      .catch((error) => onError((error as Error).message));
  }, [onError]);

  const save = async () => {
    try {
      const result = await api<{ values: Record<string, string> }>("/api/v1/admin/settings", {
        method: "PUT",
        body: JSON.stringify({ values }),
      });
      setValues(result.values);
      setSaved("Настройки сохранены и уже действуют");
      window.setTimeout(() => setSaved(""), 3000);
    } catch (error) {
      onError((error as Error).message);
    }
  };

  return (
    <>
      <PageHeading title="Настройки" text="Действуют сразу, перезапуск не нужен" />
      <nav className="admin-settings-tabs" aria-label="Разделы настроек">
        {[
          ["site", "Сайт"],
          ["marketplaces", "Маркетплейсы"],
        ].map(([id, title]) => (
          <button
            key={id}
            type="button"
            className={tab === id ? "active" : ""}
            aria-pressed={tab === id}
            onClick={() => {
              setTab(id);
              const url = new URL(window.location.href);
              url.searchParams.set("settingsTab", id);
              window.history.replaceState(null, "", url);
            }}
          >
            {title}
          </button>
        ))}
      </nav>
      <div hidden={tab !== "site"}>
        <div className="admin-settings">
          {definitions.map((definition) => (
            <label
              key={definition.key}
              className={definition.kind === "switch" ? "admin-setting switch" : "admin-setting"}
            >
              {definition.kind === "switch" ? (
                <input
                  type="checkbox"
                  role="switch"
                  aria-label={definition.title}
                  checked={values[definition.key] !== "0"}
                  onChange={(event) =>
                    setValues({ ...values, [definition.key]: event.target.checked ? "1" : "0" })
                  }
                />
              ) : null}
              <span>
                <b>
                  {definition.key === "cdek.packaging_season" ? "Сезон упаковки" : definition.title}
                </b>
                <small>
                  {definition.key === "cdek.packaging_season"
                    ? "Общий сезон для доставки сайта и расчётов WB/Ozon. Используются летние или зимние габариты товара. Сохранённые коробки заказов не меняются."
                    : definition.note}
                </small>
              </span>
              {definition.kind === "select" ? (
                <select
                  value={values[definition.key] ?? "summer"}
                  onChange={(event) =>
                    setValues({ ...values, [definition.key]: event.target.value })
                  }
                >
                  {definition.options?.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.title}
                    </option>
                  ))}
                </select>
              ) : definition.kind !== "switch" ? (
                <NumericInput
                  type={definition.kind === "number" ? "number" : "text"}
                  min="0"
                  value={values[definition.key] ?? ""}
                  onChange={(event) =>
                    setValues({ ...values, [definition.key]: event.target.value })
                  }
                />
              ) : null}
            </label>
          ))}
          <div className="admin-settings-actions">
            <button className="primary" onClick={save}>
              Сохранить
            </button>
            {saved && <span>{saved}</span>}
          </div>
        </div>
      </div>
      <div hidden={tab !== "marketplaces"}>
        <p>Общие настройки калькулятора цен. Сезон упаковки выбирается во вкладке «Сайт».</p>
        {pricingError && (
          <p role="alert">
            {pricingError}{" "}
            <button type="button" onClick={() => void loadPricing()}>
              Повторить загрузку
            </button>
          </p>
        )}
        {!pricing && !pricingError && tab === "marketplaces" && (
          <p role="status">Загружаем настройки…</p>
        )}
        {pricing && (
          <ProcurementSettingsPanel
            key={pricing.version}
            settings={pricing}
            onSaved={() => void loadPricing()}
            onError={onError}
          />
        )}
      </div>
    </>
  );
}

// The manager names the delivery price for an order the shop could not
// quote. Kept as its own component so the field holds what is being typed
// without re-rendering the whole order table on every keystroke.
export function DeliveryFeeForm({
  order,
  onSubmit,
}: {
  order: Order;
  onSubmit: (order: Order, fee: number) => void;
}) {
  const [value, setValue] = useState("");
  return (
    <form
      className="admin-fee-form"
      onSubmit={(event) => {
        event.preventDefault();
        const fee = Number(value);
        if (Number.isFinite(fee) && fee >= 0) onSubmit(order, fee);
      }}
    >
      <strong>
        {order.repackRequested
          ? "Покупатель просит упаковать в одну коробку"
          : "Доставку нужно посчитать вручную"}
      </strong>
      <p>Укажите стоимость доставки — покупатель получит уведомление и сможет оплатить заказ.</p>
      <label>
        Доставка, ₽
        <NumericInput
          min="0"
          step="1"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          placeholder="0"
          required
        />
      </label>
      <button className="primary" type="submit">
        Сохранить и уведомить
      </button>
    </form>
  );
}
