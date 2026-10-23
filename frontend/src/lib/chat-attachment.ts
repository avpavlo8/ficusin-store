import { chatPhoto } from "./chat-photo";
export type PreparedAttachment = {
  name: string;
  data: string;
  kind: "image" | "video" | "file";
  size: number;
  preview?: string;
};
export async function prepareChatAttachment(file: File): Promise<PreparedAttachment> {
  if (file.size > 20 * 1024 * 1024) throw new Error("Выберите фото, видео или файл до 20 МБ");
  if (!file.size) throw new Error("Этот файл пуст. Выберите другой");
  if (file.type.startsWith("image/")) {
    const preview = await chatPhoto(file);
    return {
      name: file.name.replace(/\.[^.]+$/, "") + ".jpg",
      data: preview.slice(preview.indexOf(",") + 1),
      kind: "image",
      size: Math.floor(preview.length * 0.75),
      preview,
    };
  }
  const source = await new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error("Не удалось прочитать файл"));
    reader.readAsDataURL(file);
  });
  return {
    name: file.name,
    data: source.slice(source.indexOf(",") + 1),
    kind: file.type.startsWith("video/") ? "video" : "file",
    size: file.size,
  };
}
export const chatFileSize = (bytes: number) =>
  bytes >= 1024 * 1024
    ? `${(bytes / (1024 * 1024)).toLocaleString("ru-RU", { maximumFractionDigits: 1 })} МБ`
    : `${Math.max(1, Math.ceil(bytes / 1024))} КБ`;
