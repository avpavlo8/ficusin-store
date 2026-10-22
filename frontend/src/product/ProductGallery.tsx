import { useEffect, useRef, useState, type TouchEvent } from "react";

const legacyPlaceholder = "/assets/hero-monstera.webp";

export function ProductGallery({ images, name, active, onSelect }: { images: string[]; name: string; active: number; onSelect: (index: number) => void }) {
  const available = images.filter((image) => Boolean(image) && image !== legacyPlaceholder && image !== "/assets/product-photo-pending.svg" && image !== "/assets/product-photo-pending-logo-v1.svg" && image !== "/assets/product-photo-pending-graphite-v1.svg");
  const [open, setOpen] = useState(false);
  const [dragX, setDragX] = useState(0);
  const [dragging, setDragging] = useState(false);
  const touchStart = useRef<{x:number;y:number;axis:"horizontal"|"vertical"|null} | null>(null);
  const swipeUntil = useRef(0);
  const move = (direction: number) => { if (available.length > 1) onSelect((active + direction + available.length) % available.length); };
  const startTouch = (event: TouchEvent) => {
    const point = event.touches.length === 1 ? event.touches[0] : undefined;
    touchStart.current = point ? {x:point.clientX,y:point.clientY,axis:null} : null;
    setDragX(0);
    setDragging(false);
  };
  const cancelTouch = () => {
    touchStart.current = null;
    setDragX(0);
    setDragging(false);
  };
  const dragTouch = (event: TouchEvent) => {
    const start = touchStart.current;
    const point = event.touches[0];
    if (!start || !point || event.touches.length !== 1 || available.length < 2) { cancelTouch(); return; }
    const dx = point.clientX-start.x;
    const dy = point.clientY-start.y;
    if (!start.axis && Math.max(Math.abs(dx),Math.abs(dy)) > 8) {
      start.axis = Math.abs(dx)>Math.abs(dy)*1.2 ? "horizontal" : "vertical";
    }
    if (start.axis !== "horizontal") return;
    const edge = (active === 0 && dx > 0) || (active === available.length-1 && dx < 0);
    setDragging(true);
    setDragX(edge ? dx*.28 : dx);
    swipeUntil.current = Date.now()+400;
  };
  const endTouch = (event: TouchEvent) => {
    const start = touchStart.current;
    const end = event.changedTouches[0];
    cancelTouch();
    if (!start || !end || event.touches.length || available.length < 2 || start.axis === "vertical") return;
    const dx = end.clientX-start.x;
    const dy = end.clientY-start.y;
    if (Math.abs(dx)>8 && Math.abs(dx)>Math.abs(dy)*1.2) {
      swipeUntil.current = Date.now()+400;
      if (Math.abs(dx)>45) onSelect(Math.max(0,Math.min(available.length-1,active+(dx>0?-1:1))));
    }
  };
  useEffect(() => { if (!open) return; const key = (event: KeyboardEvent) => { if (event.key === "ArrowLeft") move(-1); if (event.key === "ArrowRight") move(1); if (event.key === "Escape") setOpen(false); }; document.addEventListener("keydown", key); return () => document.removeEventListener("keydown", key); });
  if (!available.length) return <div className="pdp-gallery single empty" aria-label="У товара пока нет фотографии"><div className="pdp-image-placeholder" role="img" aria-label={`Фотография ${name} пока не добавлена`}><svg viewBox="0 0 64 64" aria-hidden="true"><path d="M18 50c0-17 8-30 28-36-1 21-10 31-28 36Zm0 0c6-13 14-21 26-30M18 50v7" /></svg><span>Фото скоро появится</span></div></div>;
  const photoTrack = <span className="pdp-photo-track" style={{transform:`translateX(calc(-${active*100}% + ${dragX}px))`,transition:dragging?"none":undefined}}>{available.map((image,index)=><img key={`${image}-${index}`} src={image} alt={index===active?name:""} aria-hidden={index!==active} fetchPriority={index===0?"high":"auto"} decoding="async" draggable={false} />)}</span>;
  return <div className={`pdp-gallery ${available.length === 1 ? "single" : "multiple"}`} aria-label="Фотографии товара">
    {available.length > 1 && <div className="pdp-thumbs" role="list">{available.map((image, index) => <button type="button" role="listitem" className={active === index ? "active" : ""} onClick={() => onSelect(index)} key={`${image}-${index}`} aria-label={`Фото ${index + 1} из ${available.length}`} aria-current={active === index ? "true" : undefined}><img src={image} alt="" loading="lazy" decoding="async" /></button>)}</div>}
    <button type="button" className="pdp-image" onClick={() => { if (Date.now() >= swipeUntil.current) setOpen(true); }} onTouchStart={startTouch} onTouchMove={dragTouch} onTouchEnd={endTouch} onTouchCancel={cancelTouch} aria-label="Открыть фотографию на весь экран">{photoTrack}</button>
    {available.length > 1 && <span className="pdp-image-count">{active + 1} / {available.length}</span>}
    {open && <div className="pdp-lightbox" role="dialog" aria-modal="true" aria-label={`Фотографии ${name}`} onClick={() => setOpen(false)}><header><strong>{name}</strong><span>{active + 1} / {available.length}</span><button type="button" onClick={() => setOpen(false)} aria-label="Закрыть">×</button></header><div className="pdp-lightbox-stage" onClick={(event) => event.stopPropagation()} onTouchStart={startTouch} onTouchMove={dragTouch} onTouchEnd={endTouch} onTouchCancel={cancelTouch}><button type="button" onClick={() => move(-1)} aria-label="Предыдущее фото">‹</button><span className="pdp-lightbox-viewport">{photoTrack}</span><button type="button" onClick={() => move(1)} aria-label="Следующее фото">›</button></div>{available.length > 1 && <div className="pdp-lightbox-thumbs">{available.map((image,index)=><button type="button" className={index===active?"active":""} onClick={(event)=>{event.stopPropagation();onSelect(index)}} key={`${image}-full`} aria-label={`Открыть фото ${index+1}`}><img src={image} alt="" /></button>)}</div>}</div>}
  </div>;
}
