export function ProductGallery({ images, name, active, onSelect }: { images: string[]; name: string; active: number; onSelect: (index: number) => void }) {
  const available = images.length ? images : ["/assets/hero-monstera.png"];
  return <div className="pdp-gallery" aria-label="Фотографии товара">
    {available.length > 1 && <div className="pdp-thumbs" role="list">{available.map((image, index) => <button type="button" role="listitem" className={active === index ? "active" : ""} onClick={() => onSelect(index)} key={`${image}-${index}`} aria-label={`Фото ${index + 1} из ${available.length}`} aria-current={active === index ? "true" : undefined}><img src={image} alt="" /></button>)}</div>}
    <div className="pdp-image"><img src={available[active] || available[0]} alt={name} /></div>
    {available.length > 1 && <span className="pdp-image-count">{active + 1} / {available.length}</span>}
  </div>;
}
