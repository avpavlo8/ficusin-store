export const filePayload = (file: File) =>
  new Promise<{ contentType: string; data: string }>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () =>
      resolve({ contentType: file.type, data: String(reader.result).split(",")[1] || "" });
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
