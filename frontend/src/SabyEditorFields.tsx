import { useEffect, useState } from "react";
import { api } from "./adminShared";
import { ChannelReferencePicker } from "./ChannelReferencePicker";
import type {
  ChannelCategory,
  SabyEditorConfig,
  SabyEditorField,
  SabyReadLink,
} from "./catalogEditorChannels";
export function SabyEditorFields({
  value,
  onChange,
  linked,
  barcode,
}: {
  value: SabyEditorConfig;
  onChange: (v: SabyEditorConfig, field: SabyEditorField) => void;
  linked?: Pick<SabyReadLink, "id" | "code">;
  barcode?: string;
}) {
  const [refs, setRefs] = useState<{ categories: ChannelCategory[]; folders: ChannelCategory[] }>({
    categories: [],
    folders: [],
  });
  useEffect(() => {
    let active = true;
    api<typeof refs>("/api/v1/admin/catalog-editor/references?channel=saby")
      .then((r) => {
        if (active) setRefs(r);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);
  return (
    <div className="editor-saby-fields wide">
      <div className="editor-fields">
        <ChannelReferencePicker
          label="Категория в СБИС"
          channel="saby"
          value={value.category}
          items={refs.categories}
          onChange={(category) => onChange({ ...value, category }, "category")}
        />
        <ChannelReferencePicker
          label="Раздел в СБИС"
          channel="saby"
          value={value.folder}
          items={refs.folders}
          onChange={(folder) => onChange({ ...value, folder }, "folder")}
        />
      </div>
      <div className="editor-channel-mode" role="group" aria-label="Создание или связь СБИС">
        <button
          type="button"
          className="admin-action"
          aria-pressed={value.mode === "create"}
          onClick={() => onChange({ ...value, mode: "create" }, "mode")}
        >
          Создать в СБИС
        </button>
        <button
          type="button"
          className="admin-action"
          aria-pressed={value.mode === "link"}
          onClick={() => onChange({ ...value, mode: "link" }, "mode")}
        >
          Уже создан в СБИС
        </button>
      </div>
      {value.mode === "link" && (
        <label>
          Код товара в СБИС
          <input
            value={value.existingCode}
            placeholder="X…"
            onChange={(e) => onChange({ ...value, existingCode: e.target.value }, "existingCode")}
          />
        </label>
      )}
      {barcode && <small className="editor-secondary">Штрихкод: {barcode}</small>}
      {linked?.id && (
        <small className="editor-secondary">
          {linked.code
            ? `Связь СБИС: ${linked.code}`
            : "Связь с СБИС есть. Код товара пока не найден."}
        </small>
      )}
    </div>
  );
}
