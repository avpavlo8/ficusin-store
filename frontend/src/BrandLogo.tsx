type BrandLogoProps = {
  tone?: "graphite" | "ivory";
  decorative?: boolean;
};

// Keep the approved lettering as paths; no installed font can change the logo.
export function BrandLogo({ tone = "graphite", decorative = false }: BrandLogoProps) {
  return (
    <img
      className="ficusin-logo"
      src={`/assets/brand/graphite-v1/wordmark-${tone}.svg`}
      width="1186"
      height="370"
      alt={decorative ? "" : "Фикусин"}
      draggable={false}
    />
  );
}
