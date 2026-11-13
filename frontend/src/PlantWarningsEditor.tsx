import { AutoTextarea } from "./AutoTextarea";
import { bulletWarningText, warningLines, deleteWarningLine } from "./plantWarningText";

export function PlantWarningsEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="editor-warnings wide">
      <span>Важно перед началом</span>
      <AutoTextarea
        aria-label="Важно перед началом"
        rows={4}
        value={bulletWarningText(value)}
        onKeyDown={(event) => {
          const field = event.currentTarget;
          const edit = deleteWarningLine(
            field.value,
            field.selectionStart,
            field.selectionEnd,
            event.key,
          );
          if (!edit) return;
          event.preventDefault();
          onChange(warningLines(edit.value));
          requestAnimationFrame(() => field.setSelectionRange(edit.cursor, edit.cursor));
        }}
        onChange={(event) => {
          const field = event.currentTarget;
          const cursor = bulletWarningText(field.value.slice(0, field.selectionStart)).length;
          onChange(warningLines(field.value));
          requestAnimationFrame(() => {
            if (document.activeElement === field) field.setSelectionRange(cursor, cursor);
          });
        }}
      />
    </label>
  );
}
