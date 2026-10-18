const levels: Record<string, { value: number; label: string }> = {
  easy: { value: 1, label: "Лёгкий уход" },
  medium: { value: 2, label: "Средний уход" },
  demanding: { value: 3, label: "Сложный уход" },
};

export function CareLevel({ value, shortLabel = false }: { value?: string; shortLabel?: boolean }) {
  const level = value ? levels[value] : undefined;
  if (!level) return null;
  return <span className="plant-care-level" role="img" aria-label={`Сложность ухода: ${level.label}, ${level.value} из 3`}>
    <span className="care-scale" data-level={level.value} aria-hidden="true" />
    <span>{shortLabel ? level.label.replace(" уход", "") : level.label}</span>
  </span>;
}
