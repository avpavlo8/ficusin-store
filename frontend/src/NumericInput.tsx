import {
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type Ref,
  type InputHTMLAttributes,
} from "react";

type Props = Omit<InputHTMLAttributes<HTMLInputElement>, "type" | "value" | "defaultValue"> & {
  ref?: Ref<HTMLInputElement>;
  type?: "number" | "text";
  value?: string | number;
};

// Keep numeric form constraints without browser spinners or wheel increments.
export function NumericInput({
  type = "number",
  ref: forwardedRef,
  value,
  min,
  max,
  step,
  inputMode,
  className,
  onChange,
  onFocus,
  onBlur,
  ...props
}: Props) {
  const numeric = type === "number";
  const input = useRef<HTMLInputElement>(null);
  useImperativeHandle(forwardedRef, () => input.current!, []);
  const [draft, setDraft] = useState<string | null>(null);
  const [focused, setFocused] = useState(false);
  const current = value ?? "";
  const displayed = numeric && focused && draft !== null ? draft : String(current);
  useEffect(() => {
    const field = input.current;
    if (!field) return;
    let error = "";
    if (numeric) {
      // Reuse browser range/step validation without rendering a number control.
      const validator = document.createElement("input");
      validator.type = "number";
      if (min !== undefined) validator.min = String(min);
      if (max !== undefined) validator.max = String(max);
      if (step !== undefined) validator.step = String(step);
      validator.value = displayed;
      error =
        displayed !== "" && validator.value === "" ? "Введите число" : validator.validationMessage;
    }
    field.setCustomValidity(error);
  }, [displayed, numeric, min, max, step]);
  return (
    <input
      {...props}
      ref={input}
      type="text"
      min={min}
      max={max}
      step={step}
      inputMode={
        numeric
          ? (inputMode ??
            (step !== undefined && step !== 1 && step !== "1" ? "decimal" : "numeric"))
          : inputMode
      }
      className={[numeric ? "numeric-input" : "", className].filter(Boolean).join(" ")}
      value={displayed}
      onFocus={(event) => {
        setFocused(true);
        onFocus?.(event);
      }}
      onChange={(event) => {
        if (numeric) {
          const text = event.target.value.replace(",", ".");
          const allowNegative = min === undefined || Number(min) < 0;
          if (!(allowNegative ? /^-?\d*(?:\.\d*)?$/ : /^\d*(?:\.\d*)?$/).test(text)) return;
          // Incomplete signs/decimal separators must not send NaN to the form.
          event.target.value = Number.isFinite(Number(text)) ? text : "";
          setDraft(text);
        }
        onChange?.(event);
      }}
      onBlur={(event) => {
        setFocused(false);
        setDraft(null);
        onBlur?.(event);
      }}
    />
  );
}
