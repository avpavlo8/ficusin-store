import { useEffect, useState } from "react";
import { EditorMultiSelect } from "./EditorMultiSelect";
import { api } from "./adminShared";

type Collection = {
  id: number;
  title: string;
  active: boolean;
  mode: "manual" | "dynamic";
  products: number[];
};
export function ProductCollectionsEditor({
  productId,
  membership,
  onChange,
  revision,
  disabled,
}: {
  productId?: number;
  membership: Record<string, boolean>;
  onChange: (value: Record<string, boolean>) => void;
  revision: number;
  disabled?: boolean;
}) {
  const [collections, setCollections] = useState<Collection[]>([]);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController();
    api<{ collections: Collection[] }>("/api/v1/admin/collection-definitions", {
      signal: AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
    })
      .then((data) => {
        setCollections(data.collections || []);
        setError("");
      })
      .catch((cause) => {
        if (!controller.signal.aborted)
          setError(
            (cause as Error).name === "TimeoutError"
              ? "Сервер не успел загрузить подборки. Повторите загрузку."
              : `Не удалось загрузить подборки. ${(cause as Error).message}`,
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [productId, revision, retry]);
  if (loading) return <p className="editor-secondary">Загружаем подборки…</p>;
  if (error)
    return (
      <p role="alert" className="editor-alert">
        {error}{" "}
        <button
          type="button"
          className="admin-action"
          onClick={() => {
            setLoading(true);
            setRetry((value) => value + 1);
          }}
        >
          Повторить загрузку
        </button>
      </p>
    );
  const manual = collections.filter((item) => item.mode === "manual" && item.active);
  const selected = manual.filter(
    (item) =>
      membership[String(item.id)] ?? Boolean(productId && item.products.includes(productId)),
  );
  return (
    <div className="editor-collections">
      {manual.length ? (
        <EditorMultiSelect
          label="Ручные подборки"
          options={manual.map((item) => ({ value: String(item.id), label: item.title }))}
          value={selected.map((item) => String(item.id))}
          placeholder="Выбрать подборки"
          disabled={disabled}
          onChange={(value) => {
            const next = { ...membership };
            for (const item of manual) {
              const included = value.includes(String(item.id));
              if (included === Boolean(productId && item.products.includes(productId)))
                delete next[String(item.id)];
              else next[String(item.id)] = included;
            }
            onChange(next);
          }}
        />
      ) : (
        <p className="editor-secondary">Ручных подборок пока нет.</p>
      )}
    </div>
  );
}
