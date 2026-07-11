// Original HeroPanel mark: a stylized hexagonal "H" shard. No copied branding.
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} role="img" aria-label="HeroPanel">
      <defs>
        <linearGradient id="hp-g" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="rgb(var(--brand))" />
          <stop offset="1" stopColor="rgb(var(--brand))" stopOpacity="0.6" />
        </linearGradient>
      </defs>
      <path
        d="M16 1.5 28.5 8.5v15L16 30.5 3.5 23.5v-15L16 1.5Z"
        fill="url(#hp-g)"
        stroke="rgb(var(--brand))"
        strokeWidth="1"
        strokeLinejoin="round"
      />
      <path d="M12 10v12M20 10v12M12 16h8" stroke="rgb(var(--brand-fg))" strokeWidth="2.2" strokeLinecap="round" fill="none" />
    </svg>
  );
}
