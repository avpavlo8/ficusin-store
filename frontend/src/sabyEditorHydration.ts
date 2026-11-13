import type {
  ChannelCategory,
  SabyEditorConfig,
  SabyEditorTouched,
  SabyReadLink,
} from "./catalogEditorChannels";

const emptyCategory = (): ChannelCategory => ({ id: "", name: "" });
const hasCategory = (category?: ChannelCategory) => Boolean(category?.id || category?.name);
const hasContent = (config?: SabyEditorConfig) =>
  Boolean(
    config &&
      (config.mode === "link" ||
        config.existingCode.trim() ||
        hasCategory(config.category) ||
        hasCategory(config.folder)),
  );

// Plans describe future intent; read links describe the current external card.
// Fill absent metadata only. A default empty create config is not an explicit
// request to replace an existing link; a clicked create mode always is.
export function mergeSabyEditor({
  local,
  saved,
  linked,
  touched = {},
  defaults,
}: {
  local?: SabyEditorConfig;
  saved?: SabyEditorConfig;
  linked?: SabyReadLink;
  touched?: SabyEditorTouched;
  defaults?: { category?: ChannelCategory; folder?: ChannelCategory };
}): SabyEditorConfig {
  const category = (field: "category" | "folder") => {
    if (touched[field]) return local?.[field] || emptyCategory();
    if (hasCategory(local?.[field])) return local![field];
    if (hasCategory(saved?.[field])) return saved![field];
    if (hasCategory(linked?.[field])) return linked![field];
    return (!linked?.id && defaults?.[field]) || emptyCategory();
  };
  return {
    category: category("category"),
    folder: category("folder"),
    mode:
      touched.mode && local
        ? local.mode
        : hasContent(local)
          ? local!.mode
          : hasContent(saved)
            ? saved!.mode
            : linked?.id
              ? "link"
              : local?.mode || saved?.mode || "create",
    existingCode: touched.existingCode
      ? local?.existingCode || ""
      : local?.existingCode.trim()
        ? local.existingCode
        : saved?.existingCode.trim()
          ? saved.existingCode
          : linked?.code || "",
  };
}
