type DeliveryArtProps = {
  kind: "pickup" | "cdek";
  selected: boolean;
  animationId: number;
};

export function DeliveryArt({ kind, selected, animationId }: DeliveryArtProps) {
  const isShop = kind === "pickup";
  const playing = selected && animationId > 0;
  return <span className={`checkout-delivery-art ${selected ? "is-selected" : ""}`} data-kind={kind} aria-hidden="true">
    <span className="delivery-art-frame">
      <img key={`image-${animationId}`} className={isShop ? "delivery-shop-image" : `delivery-van-image ${playing ? "is-driving" : ""}`} src={isShop ? "/assets/redesign/checkout-pickup-shop-v1.png" : "/assets/redesign/checkout-cdek-vehicle-v1.png"} alt="" width="640" height="427" draggable={false} />
      {isShop && <span key={`entrance-${animationId}`} className={`delivery-shop-entrance ${playing ? "is-opening" : ""}`}><span className="delivery-shop-door door-left" /><span className="delivery-shop-door door-right" /></span>}
    </span>
  </span>;
}
