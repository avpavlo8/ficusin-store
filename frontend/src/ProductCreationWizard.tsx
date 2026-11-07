import { useEffect, useRef, useState } from "react";
import { Dialog, api, money } from "./adminShared";
import { NumericInput } from "./NumericInput";
import { AttributeFields } from "./AttributeFields";
import type { Category, CategoryAttribute, Product } from "./adminTypes";
import "./styles/product-creation.css";

type Field = {
  code: string;
  name: string;
  type: string;
  required: boolean;
  options?: string[];
  shared?: boolean;
};
type ChannelCategory = { id: string; name: string; fields: Field[] };
export type CreationOptions = {
  enabled: boolean;
  simulation: boolean;
  sabyFolders: { id: string; name: string }[];
  wb: ChannelCategory[];
  ozon: ChannelCategory[];
};
type Channel = {
  enabled: boolean;
  category: string;
  priceMinor: number;
  values: Record<string, string>;
};
type Input = {
  step: number;
  name: string;
  latinName: string;
  description: string;
  categoryId: number;
  priceMinor: number;
  wholesaleMinQty: number;
  potCm: number;
  heightCm: number;
  packageLengthCm: number;
  packageWidthCm: number;
  packageHeightCm: number;
  packageWeightGrams: number;
  attributes: Record<string, unknown>;
  variantAttributes: Record<string, unknown>;
  photos: string[];
  sabyFolder: string;
  wb: Channel;
  ozon: Channel;
};
type Photo = { id: string; url: string; filename: string };
type Draft = {
  id: string;
  revision: number;
  simulation: boolean;
  data: Partial<Input>;
  photos: Photo[];
  product?: Product;
  results: { channel: string; externalId: string; state: string }[];
};
const steps = [
  "Основное",
  "Цена и размеры",
  "Фотографии",
  "СБИС",
  "Wildberries",
  "Ozon",
  "Проверка",
];
const emptyChannel = (): Channel => ({ enabled: false, category: "", priceMinor: 0, values: {} });
const defaults = (): Input => ({
  step: 0,
  name: "",
  latinName: "",
  description: "",
  categoryId: 0,
  priceMinor: 0,
  wholesaleMinQty: 1,
  potCm: 0,
  heightCm: 0,
  packageLengthCm: 0,
  packageWidthCm: 0,
  packageHeightCm: 0,
  packageWeightGrams: 0,
  attributes: {},
  variantAttributes: {},
  photos: [],
  sabyFolder: "",
  wb: emptyChannel(),
  ozon: emptyChannel(),
});
const known = new Set([
  "pot_diameter_cm",
  "height_cm",
  "package_length_cm",
  "package_width_cm",
  "package_height_cm",
  "package_weight_grams",
]);
const missing = (value: unknown) =>
  value === null || value === undefined || value === "" || (Array.isArray(value) && !value.length);
function issues(
  input: Input,
  step: number,
  options: CreationOptions,
  schema: CategoryAttribute[],
  schemaReady: boolean,
) {
  const errors: string[] = [];
  if (step === 0) {
    if (!input.name.trim()) errors.push("Укажите название товара");
    if (!input.latinName.trim()) errors.push("Укажите латинское название");
    if (!input.description.trim()) errors.push("Добавьте описание");
    if (!input.categoryId) errors.push("Выберите категорию сайта");
    if (!schemaReady) errors.push("Дождитесь загрузки обязательных характеристик");
    for (const field of schema.filter((x) => x.required && !known.has(x.code))) {
      const value = (field.scope === "variant" ? input.variantAttributes : input.attributes)[
        field.code
      ];
      if (missing(value)) errors.push(`Заполните: ${field.name}`);
      else if (field.dataType === "enum" && !field.options.includes(String(value)))
        errors.push(`Выберите значение: ${field.name}`);
      else if (
        field.dataType === "multi_enum" &&
        (!Array.isArray(value) || value.some((item) => !field.options.includes(String(item))))
      )
        errors.push(`Выберите значения: ${field.name}`);
    }
  }
  if (step === 1) {
    if (!(input.priceMinor > 0 && input.priceMinor <= 1_000_000_000))
      errors.push("Укажите положительную цену");
    if (
      !Number.isInteger(input.wholesaleMinQty) ||
      input.wholesaleMinQty < 1 ||
      input.wholesaleMinQty > 100000
    )
      errors.push("Минимальное количество — целое число от 1 до 100 000");
    if (!(input.potCm > 0 && input.potCm <= 500))
      errors.push("Укажите диаметр горшка от 0 до 500 см");
    if (!(input.heightCm > 0 && input.heightCm <= 3000))
      errors.push("Укажите высоту растения от 0 до 3000 см");
  }
  if (step === 2 && !input.photos.length) errors.push("Добавьте хотя бы одну фотографию");
  if (step === 3 && !options.sabyFolders.some((f) => f.id === input.sabyFolder))
    errors.push("Выберите каталог СБИС");
  if (step === 4 || step === 5) {
    const name = step === 4 ? "wb" : "ozon";
    const channel = input[name];
    if (!channel.enabled) return errors;
    const category = options[name].find((c) => c.id === channel.category);
    if (!category) errors.push("Выберите категорию площадки");
    if (!(channel.priceMinor > 0 && channel.priceMinor <= 1_000_000_000))
      errors.push("Укажите цену площадки");
    for (const field of category?.fields || []) {
      const value = channel.values[field.code]?.trim() || "";
      if (field.required && !value) errors.push(`Заполните: ${field.name}`);
      else if (value && field.options?.length && !field.options.includes(value))
        errors.push(`Выберите значение из списка: ${field.name}`);
    }
    if (
      !(
        input.packageLengthCm > 0 &&
        input.packageWidthCm > 0 &&
        input.packageHeightCm > 0 &&
        input.packageWeightGrams > 0
      )
    )
      errors.push("Укажите размеры и вес упаковки");
  }
  return errors;
}

export function ProductCreationWizard({
  options,
  onClose,
  onCreated,
}: {
  options: CreationOptions;
  onClose: () => void;
  onCreated: (product: Product) => void;
  onError: (value: string) => void;
}) {
  const [draft, setDraft] = useState<Draft | null>(null);
  const current = useRef<Draft | null>(null);
  const [data, setData] = useState<Input>(defaults);
  const [categories, setCategories] = useState<Category[]>([]);
  const [schema, setSchema] = useState<CategoryAttribute[]>([]);
  const [schemaReady, setSchemaReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const queue = useRef<Promise<unknown>>(Promise.resolve());
  const mounted = useRef(true);
  const heading = useRef<HTMLHeadingElement>(null);
  const progress = useRef<HTMLElement>(null);
  useEffect(() => {
    progress.current
      ?.querySelector<HTMLElement>('[aria-current="step"]')
      ?.scrollIntoView({ block: "nearest", inline: "center", behavior: "instant" });
  }, [data.step]);
  const endpoint = (id: string) => `/api/v1/admin/product-creations/${encodeURIComponent(id)}`;
  const receive = (next: Draft) => {
    current.current = next;
    if (mounted.current) setDraft(next);
  };
  useEffect(() => {
    mounted.current = true;
    let active = true;
    const requested = new URLSearchParams(location.search).get("creation");
    const read =
      requested && /^[a-f0-9]{32}$/.test(requested)
        ? api<Draft>(endpoint(requested))
        : api<Draft>("/api/v1/admin/product-creations", { method: "POST" });
    void Promise.all([read, api<{ categories: Category[] }>("/api/v1/admin/categories")])
      .then(([next, list]) => {
        if (!active) return;
        receive(next);
        setCategories(list.categories || []);
        const initial = {
          ...defaults(),
          ...next.data,
          attributes: next.data.attributes || {},
          variantAttributes: next.data.variantAttributes || {},
          wb: { ...emptyChannel(), ...next.data.wb, values: next.data.wb?.values || {} },
          ozon: { ...emptyChannel(), ...next.data.ozon, values: next.data.ozon?.values || {} },
        };
        initial.photos = [
          ...(initial.photos || []).filter((id) => next.photos.some((photo) => photo.id === id)),
          ...next.photos
            .filter((photo) => !(initial.photos || []).includes(photo.id))
            .map((photo) => photo.id),
        ];
        initial.step = Math.min(6, Math.max(0, initial.step || 0));
        setData(initial);
        setSaved(JSON.stringify(initial));
        const url = new URL(location.href);
        url.searchParams.set("creation", next.id);
        history.replaceState(null, "", url);
      })
      .catch((cause) => {
        if (mounted.current) setError((cause as Error).message);
      });
    return () => {
      active = false;
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    if (!data.categoryId) return;
    let active = true;
    void api<{ attributes: CategoryAttribute[] }>(
      `/api/v1/admin/categories/${data.categoryId}/attributes`,
    )
      .then((result) => {
        if (active) {
          setSchema(result.attributes || []);
          setSchemaReady(true);
        }
      })
      .catch((cause) => {
        if (active) {
          setSchemaReady(false);
          setError((cause as Error).message);
        }
      });
    return () => {
      active = false;
    };
  }, [data.categoryId]);
  const persist = (input: Input): Promise<Draft> => {
    const operation = queue.current
      .catch(() => undefined)
      .then(async () => {
        const latest = current.current;
        if (!latest || latest.product) throw new Error("Откройте незавершённый черновик");
        if (mounted.current) setSaving(true);
        try {
          const next = await api<Draft>(endpoint(latest.id), {
            method: "PUT",
            body: JSON.stringify({ revision: latest.revision, data: input }),
          });
          receive(next);
          if (mounted.current) setSaved(JSON.stringify(input));
          return next;
        } finally {
          if (mounted.current) setSaving(false);
        }
      });
    queue.current = operation;
    return operation;
  };
  const serialized = JSON.stringify(data);
  useEffect(() => {
    if (!draft || draft.product || busy || serialized === saved) return;
    const timer = window.setTimeout(() => {
      void persist(JSON.parse(serialized) as Input).catch((cause) => {
        if (mounted.current) setError((cause as Error).message);
      });
    }, 900);
    return () => window.clearTimeout(timer);
    // The queue serializes revisions; the draft object's updates must not
    // postpone a save while the operator continues editing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serialized, saved, busy, Boolean(draft), Boolean(draft?.product)]);
  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => {
      if (draft && serialized !== saved && !draft.product) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [serialized, saved, draft]);
  const clearAddress = () => {
    const url = new URL(location.href);
    url.searchParams.delete("creation");
    history.replaceState(null, "", url);
  };
  const close = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      if (draft && !draft.product && serialized !== saved) await persist(data);
      else await queue.current;
      clearAddress();
      onClose();
    } catch (cause) {
      setError((cause as Error).message);
      setBusy(false);
    }
  };
  const move = async (step: number, update = data) => {
    setBusy(true);
    setError("");
    const next = { ...update, step };
    try {
      await persist(next);
      setData(next);
      requestAnimationFrame(() => heading.current?.focus());
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const finish = async () => {
    if (!draft || busy) return;
    setBusy(true);
    setError("");
    try {
      await queue.current.catch(() => undefined);
      const existing = await api<Draft>(endpoint(draft.id));
      if (existing.product) {
        receive(existing);
        return;
      }
      const latest = await persist(data);
      receive(
        await api<Draft>(`${endpoint(draft.id)}/finish`, {
          method: "POST",
          body: JSON.stringify({ revision: latest.revision }),
        }),
      );
    } catch (cause) {
      try {
        const existing = await api<Draft>(endpoint(draft.id));
        if (existing.product) {
          receive(existing);
          return;
        }
      } catch {
        /* Keep the original error and the stable draft ID for retry. */
      }
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const change = <K extends keyof Input>(key: K, value: Input[K]) =>
    setData((previous) => ({ ...previous, [key]: value }));
  const upload = async (files: File[]) => {
    if (!files?.length || !draft || busy) return;
    if (data.photos.length + files.length > 12) {
      setError("Можно добавить до 12 фотографий");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await queue.current.catch(() => undefined);
      let selected = [...data.photos];
      for (const file of Array.from(files)) {
        if (file.size > 12 * 1024 * 1024)
          throw new Error(`${file.name}: фотография должна быть не больше 12 МБ`);
        const form = new FormData();
        form.append("file", file);
        const next = await api<Draft>(`${endpoint(draft.id)}/photos`, {
          method: "POST",
          body: form,
        });
        selected = [...new Set([...selected, ...next.photos.map((photo) => photo.id)])];
        receive(next);
        setData((previous) => ({ ...previous, photos: selected }));
      }
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const removePhoto = async (id: string) => {
    if (!draft || busy) return;
    setBusy(true);
    setError("");
    try {
      await queue.current.catch(() => undefined);
      receive(await api<Draft>(`${endpoint(draft.id)}/photos/${id}`, { method: "DELETE" }));
      setData((previous) => ({
        ...previous,
        photos: previous.photos.filter((photo) => photo !== id),
      }));
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const setChannel = (name: "wb" | "ozon", changes: Partial<Channel>) =>
    setData((previous) => ({ ...previous, [name]: { ...previous[name], ...changes } }));
  const chooseCategory = (name: "wb" | "ozon", id: string) => {
    const category = options[name].find((item) => item.id === id);
    const values: Record<string, string> = {};
    for (const field of category?.fields || []) {
      const value =
        data[name].values[field.code] ||
        (name === "ozon" && field.shared ? data.wb.values[field.code] : "") ||
        "";
      if (!field.options?.length || field.options.includes(value)) values[field.code] = value;
    }
    setChannel(name, { category: id, values });
  };
  const numeric = (
    key:
      | "wholesaleMinQty"
      | "potCm"
      | "heightCm"
      | "packageLengthCm"
      | "packageWidthCm"
      | "packageHeightCm"
      | "packageWeightGrams",
    title: string,
    min = 1,
    max = 100000,
    fractional = false,
  ) => (
    <label key={key}>
      {title}
      <NumericInput
        aria-label={title}
        min={min}
        max={max}
        step={fractional ? "any" : "1"}
        value={data[key] || ""}
        onChange={(event) => change(key, Number(event.target.value))}
      />
    </label>
  );
  const packaging = (
    <fieldset className="creation-packaging">
      <legend>Упаковка для доставки</legend>
      <p>Укажите измеренные размеры коробки и вес товара в упаковке.</p>
      <div className="admin-form-grid">
        {numeric("packageLengthCm", "Длина упаковки, см", 1, 1000)}
        {numeric("packageWidthCm", "Ширина упаковки, см", 1, 1000)}
        {numeric("packageHeightCm", "Высота упаковки, см", 1, 3000)}
        {numeric("packageWeightGrams", "Вес в упаковке, г", 1, 1000000)}
      </div>
    </fieldset>
  );
  const channelStep = (name: "wb" | "ozon") => {
    const channel = data[name];
    const title = name === "wb" ? "Wildberries" : "Ozon";
    const category = options[name].find((item) => item.id === channel.category);
    if (!channel.enabled)
      return (
        <div className="creation-channel-choice">
          <p>Создать карточку на {title} вместе с карточкой сайта? Этот шаг можно пропустить.</p>
          <button
            type="button"
            className="primary"
            onClick={() =>
              setChannel(name, {
                enabled: true,
                priceMinor:
                  channel.priceMinor ||
                  (name === "ozon" ? data.wb.priceMinor : 0) ||
                  data.priceMinor,
              })
            }
          >
            Заполнить {title}
          </button>
        </div>
      );
    return (
      <>
        <p>
          {name === "ozon"
            ? "Совместимые характеристики перенесены из Wildberries. Проверьте цену и заполните оставшиеся поля Ozon."
            : "Заполните обязательные характеристики выбранной категории."}
        </p>
        <div className="admin-form-grid">
          <label className="wide">
            Категория {title}
            <select
              aria-label={`Категория ${title}`}
              required
              value={channel.category}
              onChange={(event) => chooseCategory(name, event.target.value)}
            >
              <option value="">Выберите категорию</option>
              {options[name].map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label className="wide">
            Цена {title}, ₽
            <NumericInput
              aria-label={`Цена ${title}, ₽`}
              min="0.01"
              max="10000000"
              step="0.01"
              value={channel.priceMinor ? channel.priceMinor / 100 : ""}
              onChange={(event) =>
                setChannel(name, { priceMinor: Math.round(Number(event.target.value) * 100) })
              }
            />
          </label>
          {category?.fields.map((field) => (
            <label key={field.code}>
              {field.name}
              {field.required ? " *" : ""}
              {field.options?.length ? (
                <select
                  aria-label={`${title}: ${field.name}`}
                  required={field.required}
                  value={channel.values[field.code] || ""}
                  onChange={(event) =>
                    setChannel(name, {
                      values: { ...channel.values, [field.code]: event.target.value },
                    })
                  }
                >
                  <option value="">Выберите значение</option>
                  {field.options.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </select>
              ) : (
                <input
                  aria-label={`${title}: ${field.name}`}
                  required={field.required}
                  maxLength={1000}
                  value={channel.values[field.code] || ""}
                  onChange={(event) =>
                    setChannel(name, {
                      values: { ...channel.values, [field.code]: event.target.value },
                    })
                  }
                />
              )}
            </label>
          ))}
        </div>
        {packaging}
      </>
    );
  };
  const errors =
    data.step === 6
      ? steps.flatMap((_, step) => issues(data, step, options, schema, schemaReady))
      : issues(data, data.step, options, schema, schemaReady);
  const optional = data.step === 4 || data.step === 5;
  const selectedChannel = data.step === 4 ? "wb" : "ozon";
  const selectedPhotos = data.photos
    .map((id) => draft?.photos.find((photo) => photo.id === id))
    .filter((photo): photo is Photo => Boolean(photo));
  const done = () => {
    if (draft?.product) {
      clearAddress();
      onCreated(draft.product);
    }
  };
  return (
    <Dialog
      title={draft?.product ? "Карточки созданы в стейджинге" : "Добавить товар"}
      className="product-create-wizard"
      onClose={() => void close()}
    >
      <p className="creation-stage-note">
        Тестовый режим: рабочие СБИС, Wildberries и Ozon не меняются.
      </p>
      {draft?.product ? (
        <div className="creation-complete">
          <h2>{draft.product.name}</h2>
          <p>
            Черновик сайта №{draft.product.id} создан. Остаток — 0, карточка скрыта от покупателей.
          </p>
          <ul>
            {draft.results.map((result) => (
              <li key={result.channel}>
                <strong>
                  {{ saby: "СБИС", wb: "Wildberries", ozon: "Ozon" }[result.channel] ||
                    result.channel}
                </strong>
                <span>Тестовое создание подтверждено · {result.externalId}</span>
              </li>
            ))}
          </ul>
          {!data.wb.enabled && <p>Wildberries пропущен.</p>}
          {!data.ozon.enabled && <p>Ozon пропущен.</p>}
          <button type="button" className="primary" onClick={done}>
            Открыть карточку
          </button>
        </div>
      ) : !draft ? (
        <div className="creation-loading">
          <p role={error ? "alert" : "status"}>{error || "Открываем черновик…"}</p>
          <button type="button" onClick={() => void close()}>
            Закрыть
          </button>
        </div>
      ) : (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (!errors.length && !(optional && !data[selectedChannel].enabled)) {
              if (data.step === 6) void finish();
              else void move(data.step + 1);
            }
          }}
        >
          <div className="creation-layout">
            <nav ref={progress} className="creation-progress" aria-label="Шаги создания товара">
              <ol>
                {steps.map((title, index) => (
                  <li key={title}>
                    <button
                      type="button"
                      disabled={busy || index > data.step}
                      aria-current={data.step === index ? "step" : undefined}
                      onClick={() => {
                        if (index !== data.step) void move(index);
                      }}
                    >
                      <span>{index + 1}</span>
                      {title}
                    </button>
                  </li>
                ))}
              </ol>
            </nav>
            <section className="creation-fields" aria-labelledby="creation-step-title">
              <h2 id="creation-step-title" tabIndex={-1} ref={heading}>
                {steps[data.step]}
              </h2>
              <fieldset disabled={busy}>
                {data.step === 0 && (
                  <>
                    <p>
                      Название, описание и категория — общая основа карточек сайта, СБИС и площадок.
                    </p>
                    <div className="admin-form-grid">
                      <label className="wide">
                        Название товара
                        <input
                          aria-label="Название товара"
                          required
                          maxLength={500}
                          value={data.name}
                          onChange={(event) => change("name", event.target.value)}
                        />
                      </label>
                      <label className="wide">
                        Латинское название
                        <input
                          aria-label="Латинское название"
                          required
                          maxLength={500}
                          value={data.latinName}
                          onChange={(event) => change("latinName", event.target.value)}
                        />
                      </label>
                      <label className="wide">
                        Описание
                        <textarea
                          aria-label="Описание"
                          required
                          maxLength={10000}
                          rows={5}
                          value={data.description}
                          onChange={(event) => change("description", event.target.value)}
                        />
                      </label>
                      <label className="wide">
                        Категория сайта
                        <select
                          aria-label="Категория сайта"
                          required
                          value={data.categoryId || ""}
                          onChange={(event) => {
                            const nextCategory = Number(event.target.value);
                            if (nextCategory === data.categoryId) return;
                            setSchemaReady(false);
                            setSchema([]);
                            change("categoryId", nextCategory);
                          }}
                        >
                          <option value="">Выберите категорию</option>
                          {categories.map((category) => (
                            <option key={category.id} value={category.id}>
                              {category.name}
                            </option>
                          ))}
                        </select>
                      </label>
                      <AttributeFields
                        schema={schema.filter((field) => field.required && !known.has(field.code))}
                        values={{ ...data.attributes, ...data.variantAttributes }}
                        onChange={(code, value) => {
                          const key =
                            schema.find((field) => field.code === code)?.scope === "variant"
                              ? "variantAttributes"
                              : "attributes";
                          change(key, { ...data[key], [code]: value });
                        }}
                      />
                    </div>
                  </>
                )}
                {data.step === 1 && (
                  <>
                    <p>
                      Цена хранится на сайте. Остаток при создании равен нулю и появится после учёта
                      поступления.
                    </p>
                    <div className="admin-form-grid">
                      <label className="wide">
                        Цена на сайте, ₽
                        <NumericInput
                          aria-label="Цена на сайте, ₽"
                          required
                          min="0.01"
                          max="10000000"
                          step="0.01"
                          value={data.priceMinor ? data.priceMinor / 100 : ""}
                          onChange={(event) =>
                            change("priceMinor", Math.round(Number(event.target.value) * 100))
                          }
                        />
                      </label>
                      {numeric("wholesaleMinQty", "Минимальное количество для опта, шт")}
                      {numeric("potCm", "Диаметр горшка, см", 0.1, 500, true)}
                      {numeric("heightCm", "Высота растения, см", 0.1, 3000, true)}
                    </div>
                    {packaging}
                  </>
                )}
                {data.step === 2 && (
                  <>
                    <p>
                      Добавьте реальные фотографии товара. Первая фотография станет обложкой для
                      сайта и площадок.
                    </p>
                    <label className="creation-upload">
                      Добавить фотографии
                      <input
                        aria-label="Добавить фотографии"
                        type="file"
                        accept="image/jpeg,image/png,image/gif"
                        multiple
                        onChange={(event) => {
                          void upload(Array.from(event.target.files || []));
                          event.target.value = "";
                        }}
                      />
                    </label>
                    <p>До 12 фотографий, каждая не больше 12 МБ.</p>
                    <ol className="creation-photos">
                      {selectedPhotos.map((photo, index) => (
                        <li key={photo.id}>
                          <img src={photo.url} alt={`Фото товара ${index + 1}`} />
                          <span>{index === 0 ? "Обложка" : `Фото ${index + 1}`}</span>
                          {index > 0 && (
                            <button
                              type="button"
                              onClick={() =>
                                change("photos", [
                                  photo.id,
                                  ...data.photos.filter((id) => id !== photo.id),
                                ])
                              }
                            >
                              Сделать обложкой
                            </button>
                          )}
                          <button
                            type="button"
                            aria-label={`Удалить фото ${index + 1}`}
                            onClick={() => void removePhoto(photo.id)}
                          >
                            Удалить
                          </button>
                        </li>
                      ))}
                    </ol>
                  </>
                )}
                {data.step === 3 && (
                  <>
                    <p>
                      Название, цена, горшок и высота уже заполнены. Выберите, куда добавить
                      карточку в СБИС.
                    </p>
                    <dl className="creation-review">
                      <dt>Название</dt>
                      <dd>{data.name}</dd>
                      <dt>Цена</dt>
                      <dd>{money.format(data.priceMinor / 100)}</dd>
                      <dt>Размер</dt>
                      <dd>
                        Горшок {data.potCm} см · высота {data.heightCm} см
                      </dd>
                    </dl>
                    <label>
                      Каталог СБИС
                      <select
                        aria-label="Каталог СБИС"
                        required
                        value={data.sabyFolder}
                        onChange={(event) => change("sabyFolder", event.target.value)}
                      >
                        <option value="">Выберите каталог</option>
                        {options.sabyFolders.map((folder) => (
                          <option key={folder.id} value={folder.id}>
                            {folder.name}
                          </option>
                        ))}
                      </select>
                    </label>
                  </>
                )}
                {data.step === 4 && channelStep("wb")}
                {data.step === 5 && channelStep("ozon")}
                {data.step === 6 && (
                  <>
                    <p>
                      Проверьте данные. После нажатия «Создать» появится один черновик сайта и
                      тестовые карточки выбранных площадок.
                    </p>
                    <dl className="creation-review">
                      <dt>Название</dt>
                      <dd>{data.name}</dd>
                      <dt>Латинское название</dt>
                      <dd>{data.latinName}</dd>
                      <dt>Описание</dt>
                      <dd>{data.description}</dd>
                      <dt>Категория сайта</dt>
                      <dd>{categories.find((item) => item.id === data.categoryId)?.name}</dd>
                      <dt>Цена сайта</dt>
                      <dd>{money.format(data.priceMinor / 100)}</dd>
                      <dt>Минимум для опта</dt>
                      <dd>{data.wholesaleMinQty} шт</dd>
                      <dt>Фотографии</dt>
                      <dd>{selectedPhotos.length} шт</dd>
                      <dt>СБИС</dt>
                      <dd>
                        {options.sabyFolders.find((item) => item.id === data.sabyFolder)?.name}
                      </dd>
                      <dt>Wildberries</dt>
                      <dd>
                        {data.wb.enabled
                          ? `${money.format(data.wb.priceMinor / 100)} · тестовая карточка`
                          : "Пропущен"}
                      </dd>
                      <dt>Ozon</dt>
                      <dd>
                        {data.ozon.enabled
                          ? `${money.format(data.ozon.priceMinor / 100)} · тестовая карточка`
                          : "Пропущен"}
                      </dd>
                    </dl>
                    {selectedPhotos[0] && (
                      <img
                        className="creation-review-photo"
                        src={selectedPhotos[0].url}
                        alt="Обложка товара"
                      />
                    )}
                  </>
                )}
              </fieldset>
              {errors.length > 0 && (
                <ul className="creation-missing" aria-label="Что осталось заполнить">
                  {errors.map((issue, index) => (
                    <li key={index}>{issue}</li>
                  ))}
                </ul>
              )}
              {error && (
                <p role="alert" className="admin-inline-error">
                  {error}
                </p>
              )}
            </section>
          </div>
          <footer className="creation-actions">
            <span role="status">
              {saving
                ? "Сохраняем черновик…"
                : saved === serialized
                  ? "Черновик сохранён"
                  : "Есть несохранённые изменения"}
            </span>
            <div>
              <button
                type="button"
                disabled={busy}
                onClick={() => {
                  if (data.step > 0) void move(data.step - 1);
                  else void close();
                }}
              >
                {data.step > 0 ? "Назад" : "Продолжить позже"}
              </button>
              {optional && (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() =>
                    void move(data.step + 1, {
                      ...data,
                      [selectedChannel]: { ...data[selectedChannel], enabled: false },
                    })
                  }
                >
                  Пропустить {data.step === 4 ? "Wildberries" : "Ozon"}
                </button>
              )}
              <button
                type="submit"
                className="primary"
                disabled={busy || errors.length > 0 || (optional && !data[selectedChannel].enabled)}
              >
                {busy ? "Сохраняем…" : data.step === 6 ? "Создать" : "Далее"}
              </button>
            </div>
          </footer>
        </form>
      )}
    </Dialog>
  );
}
