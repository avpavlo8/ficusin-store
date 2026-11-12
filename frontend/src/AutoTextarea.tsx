import { useLayoutEffect, useRef, type TextareaHTMLAttributes } from "react";

// Long text belongs to the editor's scroll area, rather than a nested scroller.
export function AutoTextarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  const field = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const node = field.current;
    if (!node) return;
    const fit = () => {
      node.style.height = "auto";
      const style = getComputedStyle(node);
      const border = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
      node.style.height = `${node.scrollHeight + border}px`;
    };
    fit();
    let previousWidth = node.getBoundingClientRect().width;
    const observer = new ResizeObserver(() => {
      const width = node.getBoundingClientRect().width;
      if (width !== previousWidth) {
        previousWidth = width;
        fit();
      }
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [props.value, props.defaultValue, props.rows]);
  return (
    <textarea
      {...props}
      ref={field}
      style={{ ...props.style, resize: "none", overflowY: "hidden" }}
    />
  );
}
