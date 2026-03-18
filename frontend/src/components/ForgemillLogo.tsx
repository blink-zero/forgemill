interface ForgemillLogoProps {
  className?: string;
  size?: number;
}

/**
 * Forgemill brand logo mark — geometric "F" with a forge spark accent.
 * Uses the app icon style (dark rounded square background).
 */
export function ForgemillLogo({ className, size = 24 }: ForgemillLogoProps) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 100 100"
      width={size}
      height={size}
      className={className}
      aria-label="Forgemill"
      role="img"
    >
      {/* Background */}
      <rect width="100" height="100" rx="20" fill="#0f172a" />

      {/* F vertical stem */}
      <rect x="22" y="18" width="13" height="64" rx="2.5" fill="#3b82f6" />
      {/* F top horizontal bar */}
      <rect x="22" y="18" width="44" height="13" rx="2.5" fill="#3b82f6" />
      {/* F mid horizontal bar */}
      <rect x="22" y="41" width="33" height="11" rx="2.5" fill="#3b82f6" />

      {/* Forge spark — glowing hot tip at end of mid bar */}
      <circle cx="57" cy="46.5" r="7" fill="#ffffff" opacity="0.15" />
      <circle cx="57" cy="46.5" r="4.5" fill="#ffffff" />
      <circle cx="57" cy="46.5" r="2" fill="#ffffff" />
    </svg>
  );
}
