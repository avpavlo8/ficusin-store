export function warningLines(text: string): string {
  return text
    .split(/\r?\n/)
    .map((line) => line.replace(/^\s*(?:[•●▪]\s*|[-*]\s+)/, ""))
    .join("\n");
}
export function bulletWarningText(text: string): string {
  return warningLines(text)
    .split("\n")
    .map((line) => `• ${line}`)
    .join("\n");
}

export function deleteWarningLine(
  text: string,
  start: number,
  end: number,
  key: string,
): { value: string; cursor: number } | null {
  if (start !== end) return null;
  const lineStart = text.lastIndexOf("\n", start - 1) + 1;
  if (
    key === "Backspace" &&
    lineStart > 0 &&
    /^(?:[•●▪]?[ \t]*)$/.test(text.slice(lineStart, start))
  ) {
    const marker = text.slice(lineStart).match(/^[•●▪][ \t]?/);
    const removeThrough = Math.max(start, lineStart + (marker?.[0].length || 0));
    return {
      value: text.slice(0, lineStart - 1) + text.slice(removeThrough),
      cursor: lineStart - 1,
    };
  }
  if (key === "Delete" && text[start] === "\n") {
    const marker = text.slice(start + 1).match(/^[•●▪][ \t]?/);
    return {
      value: text.slice(0, start) + text.slice(start + 1 + (marker?.[0].length || 0)),
      cursor: start,
    };
  }
  return null;
}
