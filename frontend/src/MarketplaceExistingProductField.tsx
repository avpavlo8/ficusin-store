import { useRef, useState } from "react";
import { api } from "./adminShared";
export function MarketplaceExistingProductField({
  channel,
  value,
  onChange,
}: {
  channel: "wb" | "ozon";
  value: string;
  onChange: (value: string) => void;
}) {
  const [result, setResult] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const sequence = useRef(0);
  const title = channel === "wb" ? "Wildberries" : "Ozon";
  const check = async () => {
    const seq = ++sequence.current;
    setBusy(true);
    setError("");
    setResult("");
    try {
      const r = await api<{ data: { card: { productId: string; offerId: string; sku?: string } } }>(
        `/api/v1/admin/catalog-editor/references?channel=${channel}&kind=existing&q=${encodeURIComponent(value.trim())}`,
      );
      if (seq !== sequence.current) return;
      setResult(`Найден товар ${r.data.card.offerId} · ID ${r.data.card.productId}`);
    } catch (e) {
      if (seq === sequence.current) setError((e as Error).message);
    } finally {
      if (seq === sequence.current) setBusy(false);
    }
  };
  return (
    <div className="editor-existing-card">
      <label>
        Артикул
        <input
          value={value}
          aria-label={`Артикул ${title}`}
          placeholder={
            channel === "wb"
              ? "Артикул WB или артикул продавца"
              : "Артикул продавца, sku:… или id:…"
          }
          onChange={(e) => {
            sequence.current++;
            setBusy(false);
            onChange(e.target.value);
            setResult("");
            setError("");
          }}
        />
      </label>
      <button
        className="admin-action"
        type="button"
        disabled={busy || !value.trim()}
        onClick={() => void check()}
      >
        {busy ? "Проверяем…" : "Проверить товар"}
      </button>
      {result && (
        <p className="editor-existing-found" role="status">
          ✓ {result}
        </p>
      )}
      {error && (
        <p className="editor-secondary" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
