export const returnMoney = (price = 0) => price.toLocaleString("ru-RU", { maximumFractionDigits: 2 }) + " ₽";
export async function downloadReturnLabels(ids: number[], includeDate = true) {
  const response = await fetch("/api/v1/admin/returns/labels", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ids, includeDate }) });
  if (!response.ok) { const result = await response.json(); throw new Error(result.error || "Не удалось скачать этикетки"); }
  const url = URL.createObjectURL(await response.blob()); const link = document.createElement("a"); link.href = url; link.download = "Этикетки-на-восстановление.pdf"; link.click(); window.setTimeout(() => URL.revokeObjectURL(url), 10000);
}
export async function returnPhotoBase64(file: File): Promise<string> {
  let bitmap: ImageBitmap;
  try { bitmap = await createImageBitmap(file); } catch { throw new Error("Не удалось прочитать фото. Выберите JPEG, PNG или WebP."); }
  const scale = Math.min(1, 2000 / Math.max(bitmap.width, bitmap.height));
  const canvas = document.createElement("canvas"); canvas.width = Math.round(bitmap.width * scale); canvas.height = Math.round(bitmap.height * scale);
  const context = canvas.getContext("2d"); if (!context) { bitmap.close(); throw new Error("Не удалось подготовить фото"); }
  context.fillStyle = "#fff"; context.fillRect(0, 0, canvas.width, canvas.height); context.drawImage(bitmap, 0, 0, canvas.width, canvas.height); bitmap.close();
  return canvas.toDataURL("image/jpeg", .88).split(",")[1];
}
