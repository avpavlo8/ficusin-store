import { useEffect, useRef, useState } from "react";

const legacyPlaceholder = "/assets/hero-monstera.webp";

export function ProductGallery({ images, name, active, onSelect }: { images: string[]; name: string; active: number; onSelect: (index: number) => void }) {
  // Old backend versions substituted the homepage monstera when a product had
  // no media. Treat that sentinel as missing media so a customer never sees a
  // different plant as the product photo while deployments overlap.
  const available = images.filter((image) => Boolean(image) && image !== legacyPlaceholder);
  const [open, setOpen] = useState(false);
  const touchStart = useRef<number | null>(null);
  const move = (direction: number) => { if (available.length > 1) onSelect((active + direction + available.length) % available.length); };
  useEffect(() => { if (!open) return; const key = (event: KeyboardEvent) => { if (event.key === "ArrowLeft") move(-1); if (event.key === "ArrowRight") move(1); if (event.key === "Escape") setOpen(false); }; document.addEventListener("keydown", key); return () => document.removeEventListener("keydown", key); });
  if (!available.length) return <div className="pdp-gallery single empty" aria-label="У товара пока нет фотографии"><div className="pdp-image-placeholder" role="img" aria-label={`Фотография ${name} пока не добавлена`}><svg viewBox="0 0 64 64" aria-hidden="true"><path d="M18 50c0-17 8-30 28-36-1 21-10 31-28 36Zm0 0c6-13 14-21 26-30M18 50v7" /></svg><span>Фото скоро появится</span></div></div>;
  return <div className={`pdp-gallery ${available.length === 1 ? "single" : "multiple"}`} aria-label="Фотографии товара">
    {available.length > 1 && <div className="pdp-thumbs" role="list">{available.map((image, index) => <button type="button" role="listitem" className={active === index ? "active" : ""} onClick={() => onSelect(index)} key={`${image}-${index}`} aria-label={`Фото ${index + 1} из ${available.length}`} aria-current={active === index ? "true" : undefined}><img src={image} alt="" loading="lazy" decoding="async" /></button>)}</div>}
    <button type="button" className="pdp-image" onClick={() => setOpen(true)} aria-label="Открыть фотографию на весь экран"><img src={available[active] || available[0]} alt={`${name}, фото ${active + 1}`} width="900" height="900" fetchPriority="high" decoding="async" /></button>
    {available.length > 1 && <span className="pdp-image-count">{active + 1} / {available.length}</span>}
    {open && <div className="pdp-lightbox" role="dialog" aria-modal="true" aria-label={`Фотографии ${name}`} onClick={() => setOpen(false)}><header><strong>{name}</strong><span>{active + 1} / {available.length}</span><button type="button" onClick={() => setOpen(false)} aria-label="Закрыть">×</button></header><div className="pdp-lightbox-stage" onClick={(event) => event.stopPropagation()} onTouchStart={(event) => { touchStart.current = event.touches[0]?.clientX ?? null; }} onTouchEnd={(event) => { if (touchStart.current === null) return; const distance = (event.changedTouches[0]?.clientX ?? touchStart.current) - touchStart.current; if (Math.abs(distance) > 45) move(distance > 0 ? -1 : 1); touchStart.current = null; }}>{available.length > 1 && <button type="button" onClick={() => move(-1)} aria-label="Предыдущее фото">‹</button>}<img src={available[active] || available[0]} alt={`${name}, фото ${active + 1}`} width="1200" height="1200" />{available.length > 1 && <button type="button" onClick={() => move(1)} aria-label="Следующее фото">›</button>}</div>{available.length > 1 && <div className="pdp-lightbox-thumbs">{available.map((image,index)=><button type="button" className={index===active?"active":""} onClick={(event)=>{event.stopPropagation();onSelect(index)}} key={`${image}-full`} aria-label={`Открыть фото ${index+1}`}><img src={image} alt="" width="80" height="80" /></button>)}</div>}</div>}
  </div>;
}