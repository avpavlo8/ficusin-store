import type { RemoteMarketplaceCard } from "./marketplaceAutofill";
import { MarketplaceSellerArticle } from "./MarketplaceSellerArticle";
import { MarketplaceExistingProductField } from "./MarketplaceExistingProductField";
import { attributeRole } from "./marketplaceAttributes";
import { useEffect, useState } from "react";
import { AutoTextarea } from "./AutoTextarea";
import { NumericInput } from "./NumericInput";
import { ChannelReferencePicker } from "./ChannelReferencePicker";
import { api } from "./adminShared";
import { MarketplaceAttributeField, MarketplaceDictionary } from "./MarketplaceAttributeField";
import type { MarketplaceCommon, MarketplaceChannel, OzonAttribute } from "./catalogEditorChannels";

function AdditionalAttributes({
  channel,
  value,
  onChange,
}: {
  channel: "ozon" | "wb";
  value: MarketplaceChannel;
  onChange: (v: MarketplaceChannel) => void;
}) {
  const [attributes, setAttributes] = useState<OzonAttribute[]>([]),
    [error, setError] = useState("");
  const id = value.category.id,
    typeId = value.category.typeId || 0;
  useEffect(() => {
    if (!id || value.mode === "link") return;
    let active = true;
    api<{ data: { result?: OzonAttribute[]; data?: OzonAttribute[] } }>(
      `/api/v1/admin/catalog-editor/references?channel=${channel}&kind=attributes&category=${id}&typeId=${typeId}`,
    )
      .then((r) => {
        if (active) {
          setAttributes(r.data.result || r.data.data || []);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [channel, id, typeId, value.mode]);
  const extra = attributes.filter(
    (a) =>
      (a.is_required || a.required) &&
      !attributeRole(a, channel) &&
      !/диаметр.*горш|диаметр.*кашпо/i.test(a.name),
  );
  return (
    <>
      {error && <p className="editor-secondary">{error}</p>}
      {extra.map((a) => (
        <MarketplaceAttributeField
          key={`${id}:${typeId}:${a.id || a.charcID}`}
          channel={channel}
          category={value.category}
          attribute={a}
          value={value.attributes?.[String(a.id || a.charcID)]}
          onChange={(newValue) =>
            onChange({
              ...value,
              attributes: { ...value.attributes, [String(a.id || a.charcID)]: newValue },
            })
          }
        />
      ))}
    </>
  );
}
function DictionaryField({
  label,
  kind,
  value,
  id,
  marketplace,
  onChange,
}: {
  label: string;
  kind: "brand" | "tnved";
  value: string;
  id?: number;
  marketplace: MarketplaceCommon;
  onChange: (value: string, id?: number) => void;
}) {
  const channel =
    marketplace.ozon.mode === "create" &&
    (marketplace.ozon.enabled || !!marketplace.ozon.category.id || !marketplace.wb.category.id)
      ? "ozon"
      : "wb";
  const category = marketplace[channel].category;
  const [attribute, setAttribute] = useState<number>();
  const [error, setError] = useState("");
  useEffect(() => {
    if (channel !== "ozon" || !category.id) return;
    let active = true;
    api<{ data: { result: OzonAttribute[] } }>(
      `/api/v1/admin/catalog-editor/references?channel=ozon&kind=attributes&category=${category.id}&typeId=${category.typeId || 0}`,
    )
      .then((r) => {
        if (active) {
          setAttribute(r.data.result.find((a) => attributeRole(a, "ozon") === kind)?.id);
          setError("");
        }
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [channel, category.id, category.typeId, kind]);
  return (
    <div>
      {channel === "ozon" && !attribute ? (
        <label>
          {label} *
          <input
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder="Сначала выберите категорию Ozon"
          />
        </label>
      ) : (
        <MarketplaceDictionary
          key={`${channel}:${category.id}:${category.typeId}:${attribute}`}
          label={`${label} *`}
          channel={channel}
          category={category}
          kind={channel === "ozon" ? "values" : kind}
          attribute={attribute}
          selected={value ? [{ value, id }] : []}
          onChange={(values) =>
            onChange(values[0]?.value || "", channel === "ozon" ? values[0]?.id : undefined)
          }
        />
      )}
      {error && (
        <p className="editor-secondary" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
export function MarketplaceEditor({
  linked = {},
  productId,
  variantId,
  value,
  onChange,
  onCardResolved,
  name,
  description,
  prices,
}: {
  linked?: Partial<Record<"wb" | "ozon", string>>;
  productId?: number;
  variantId?: number;
  value: MarketplaceCommon;
  onChange: (value: MarketplaceCommon) => void;
  onCardResolved: (channel: "wb" | "ozon", card: RemoteMarketplaceCard) => void;
  name: string;
  description: string;
  prices: { ozon: string; wb: string };
}) {
  const patch = (p: Partial<MarketplaceCommon>) => onChange({ ...value, ...p });
  const showFields = (channel: "wb" | "ozon") =>
    !!linked[channel] || value[channel].mode === "create";
  const hasCreate = showFields("wb") || showFields("ozon");
  return (
    <>
      <table className="editor-marketplace-table" aria-label="Поля Wildberries и Ozon">
        <thead>
          <tr>
            <th scope="col">Wildberries</th>
            <th scope="col">Ozon</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            {(["wb", "ozon"] as const).map((channel) => {
              const ch = value[channel],
                title = channel === "wb" ? "Wildberries" : "Ozon";
              const change = (v: MarketplaceChannel) => patch({ [channel]: v });
              return (
                <td key={channel}>
                  {linked[channel] === "!ambiguous" && (
                    <p className="editor-secondary">
                      Найдено несколько связей. Проверьте соответствие карточек.
                    </p>
                  )}
                  {!linked[channel] && (
                    <div
                      className="editor-channel-mode"
                      role="group"
                      aria-label={`Создание или связь ${title}`}
                    >
                      <button
                        type="button"
                        className="admin-action"
                        aria-pressed={ch.mode === "create"}
                        onClick={() => change({ ...ch, mode: "create" })}
                      >
                        Создать новый
                      </button>
                      <button
                        type="button"
                        className="admin-action"
                        aria-pressed={ch.mode === "link"}
                        onClick={() => change({ ...ch, mode: "link" })}
                      >
                        Уже есть на {title}
                      </button>
                    </div>
                  )}
                  {showFields(channel) && (
                    <MarketplaceSellerArticle
                      key={`${channel}:${variantId}`}
                      channel={channel}
                      value={ch}
                      fallback={value.article}
                      linked={!!linked[channel]}
                      productId={productId}
                      variantId={variantId}
                      onChange={change}
                      onResolved={(remote) => onCardResolved(channel, remote)}
                    />
                  )}
                  {!linked[channel] && ch.mode === "link" ? (
                    <MarketplaceExistingProductField
                      key={channel}
                      channel={channel}
                      value={ch.existingId}
                      onChange={(existingId) => change({ ...ch, existingId })}
                    />
                  ) : null}
                </td>
              );
            })}
          </tr>
          {hasCreate && (
            <>
              <tr>
                {!showFields("wb") && (
                  <td className="editor-marketplace-empty" aria-hidden="true" />
                )}
                <td colSpan={showFields("wb") && showFields("ozon") ? 2 : 1}>
                  <div className="editor-fields">
                    <label className="wide">
                      Название
                      <AutoTextarea
                        value={value.contentFromMarketplace ? value.name : value.name || name}
                        rows={1}
                        onChange={(e) => patch({ name: e.target.value })}
                      />
                    </label>
                  </div>
                </td>
                {!showFields("ozon") && (
                  <td className="editor-marketplace-empty" aria-hidden="true" />
                )}
              </tr>
              <tr>
                {(["wb", "ozon"] as const).map((channel) => (
                  <td
                    key={channel}
                    className={!showFields(channel) ? "editor-marketplace-empty" : undefined}
                  >
                    {showFields(channel) && (
                      <ChannelReferencePicker
                        channel={channel}
                        label="Категория *"
                        ariaLabel={`Категория ${channel === "wb" ? "Wildberries" : "Ozon"} *`}
                        value={value[channel].category}
                        onChange={(category) =>
                          patch({ [channel]: { ...value[channel], category, attributes: {} } })
                        }
                      />
                    )}
                  </td>
                ))}
              </tr>
              <tr>
                {(["wb", "ozon"] as const).map((channel) => (
                  <td
                    key={channel}
                    className={!showFields(channel) ? "editor-marketplace-empty" : undefined}
                  >
                    {showFields(channel) && (
                      <div className="editor-fields editor-marketplace-specific">
                        <label>
                          Цена {channel === "wb" ? "Wildberries" : "Ozon"}, ₽
                          <NumericInput
                            readOnly
                            value={prices[channel]}
                            placeholder="Рассчитывается в «Товаре»"
                          />
                        </label>
                      </div>
                    )}
                  </td>
                ))}
              </tr>
              <tr>
                {!showFields("wb") && (
                  <td className="editor-marketplace-empty" aria-hidden="true" />
                )}
                <td colSpan={showFields("wb") && showFields("ozon") ? 2 : 1}>
                  <div className="editor-fields">
                    <label className="wide">
                      Описание / аннотация
                      <AutoTextarea
                        rows={5}
                        value={
                          value.contentFromMarketplace
                            ? value.description
                            : value.description || description
                        }
                        onChange={(e) => patch({ description: e.target.value })}
                      />
                    </label>
                  </div>
                </td>
                {!showFields("ozon") && (
                  <td className="editor-marketplace-empty" aria-hidden="true" />
                )}
              </tr>
              <tr>
                {!showFields("wb") && (
                  <td className="editor-marketplace-empty" aria-hidden="true" />
                )}
                <td colSpan={showFields("wb") && showFields("ozon") ? 2 : 1}>
                  <div className="editor-fields">
                    <DictionaryField
                      key={`brand:${value.ozon.category.id}:${value.ozon.category.typeId}:${value.ozon.mode}`}
                      label="Бренд"
                      kind="brand"
                      value={value.brand}
                      id={value.brandId}
                      marketplace={value}
                      onChange={(brand, brandId) => patch({ brand, brandId })}
                    />
                    <DictionaryField
                      key={`tnved:${value.ozon.category.id}:${value.ozon.category.typeId}:${value.ozon.mode}`}
                      label="ТН ВЭД"
                      kind="tnved"
                      value={value.tnved}
                      id={value.tnvedId}
                      marketplace={value}
                      onChange={(tnved, tnvedId) => patch({ tnved, tnvedId })}
                    />
                    <label>
                      НДС
                      <input readOnly value="Без НДС" />
                    </label>
                  </div>
                </td>
                {!showFields("ozon") && (
                  <td className="editor-marketplace-empty" aria-hidden="true" />
                )}
              </tr>
              <tr>
                {(["wb", "ozon"] as const).map((channel) => {
                  const ch = value[channel];
                  const change = (v: MarketplaceChannel) => patch({ [channel]: v });
                  return (
                    <td
                      key={channel}
                      className={!showFields(channel) ? "editor-marketplace-empty" : undefined}
                    >
                      {showFields(channel) && (
                        <div className="editor-fields editor-marketplace-specific">
                          {channel === "ozon" && (
                            <>
                              <label>
                                Название модели *
                                <input
                                  value={value.model}
                                  onChange={(e) => patch({ model: e.target.value })}
                                />
                              </label>
                              <label>
                                Единиц в одном товаре *
                                <NumericInput
                                  min="1"
                                  step="1"
                                  value={value.units || ""}
                                  onChange={(e) => patch({ units: Number(e.target.value) })}
                                />
                              </label>
                            </>
                          )}
                          <AdditionalAttributes
                            key={`${channel}:${ch.category.id}:${ch.category.typeId}`}
                            channel={channel}
                            value={ch}
                            onChange={change}
                          />
                        </div>
                      )}
                    </td>
                  );
                })}
              </tr>
            </>
          )}
        </tbody>
      </table>
    </>
  );
}
