import { useState, type InputHTMLAttributes } from "react";

// Keep a cleared field and leading zeros while typing. API values remain
// numbers; range checks belong to the form, never silently clamp user input.
export function ReturnNumberInput({
  value,
  onValue,
  ...props
}: Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "value" | "onChange"> & {
  value: number;
  onValue: (value: number) => void;
}) {
  const [edit, setEdit] = useState({ number: value, text: String(value) });
  const text = edit.number === value ? edit.text : String(value);
  return (
    <input
      {...props}
      type="text"
      inputMode="numeric"
      pattern="[0-9]*"
      value={text}
      onChange={(event) => {
        const text = event.target.value;
        if (!/^\d*$/.test(text)) return;
        const number = Number(text);
        setEdit({ number, text });
        onValue(number);
      }}
      onBlur={() => setEdit({ number: value, text: String(value) })}
    />
  );
}
