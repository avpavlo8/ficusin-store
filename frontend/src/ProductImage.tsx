import { useState, type ImgHTMLAttributes } from "react";

const placeholder = "/assets/product-photo-pending-graphite-v1.svg";

function webImageSource(src: string): string {
  try {
    const url = new URL(src, window.location.href);
    if (url.protocol === "http:" || url.protocol === "https:") {
      return src.replace(/[<>"`]/g, (character) => encodeURIComponent(character));
    }
  } catch {
    return placeholder;
  }
  return placeholder;
}

export function ProductImage({ src, alt, onError, ...props }: ImgHTMLAttributes<HTMLImageElement>) {
  const [failedSource, setFailedSource] = useState<string>();
  return (
    <img
      {...props}
      src={!src || failedSource === src ? placeholder : webImageSource(src)}
      alt={alt}
      onError={(event) => {
        if (failedSource !== src) setFailedSource(src);
        onError?.(event);
      }}
    />
  );
}
