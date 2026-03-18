/**
 * Provider icon component for VMware vCenter, ESXi, and Proxmox targets.
 * Uses inline SVG logos for crisp rendering at any size.
 */

interface ProviderIconProps {
  type: "vcenter" | "esxi" | "proxmox" | string;
  className?: string;
  size?: number;
}

function VCenterIcon({ size = 20, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-label="vCenter">
      <rect x="2" y="2" width="20" height="20" rx="4" fill="#696566" />
      <path d="M6.5 8L12 16L17.5 8" stroke="white" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function ESXiIcon({ size = 20, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-label="ESXi">
      <rect x="2" y="2" width="20" height="20" rx="4" fill="#78BE20" />
      <path d="M7 7H17M7 12H14M7 17H17" stroke="white" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}

function ProxmoxIcon({ size = 20, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" className={className} aria-label="Proxmox">
      <rect x="2" y="2" width="20" height="20" rx="4" fill="#E57000" />
      <circle cx="12" cy="12" r="5" stroke="white" strokeWidth="2" />
      <circle cx="12" cy="12" r="1.5" fill="white" />
    </svg>
  );
}

export default function ProviderIcon({ type, className, size = 20 }: ProviderIconProps) {
  switch (type) {
    case "vcenter":
      return <VCenterIcon size={size} className={className} />;
    case "esxi":
      return <ESXiIcon size={size} className={className} />;
    case "proxmox":
      return <ProxmoxIcon size={size} className={className} />;
    default:
      return <span className={className} title={type}>🖥️</span>;
  }
}

/** Returns a human-readable label for a provider type */
export function providerLabel(type: string): string {
  switch (type) {
    case "vcenter": return "vCenter";
    case "esxi": return "ESXi";
    case "proxmox": return "Proxmox";
    default: return type;
  }
}
