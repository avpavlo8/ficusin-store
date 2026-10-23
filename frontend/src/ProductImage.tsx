import { useState, type ImgHTMLAttributes } from "react";

const placeholder = "/assets/product-photo-pending-graphite-v1.svg";

export function ProductImage({ src, alt, onError, ...props }: ImgHTMLAttributes<HTMLImageElement>) {
  const [failedSource, setFailedSource] = useState<string>();
  return (
    <img
      {...props}
      src={!src || failedSource === src ? placeholder : src}
      alt={alt}
      onError={(event) => {
        if (failedSource !== src) setFailedSource(src);
        onError?.(event);
      }}
    />
  );
}
