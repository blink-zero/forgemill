/**
 * Provider icons for VMware vCenter / ESXi and Proxmox VE targets.
 *
 * The marks are the vendors' own: the Proxmox "X" mark and the VMware
 * wordmark, path data from Simple Icons (CC0, https://simpleicons.org),
 * rendered unaltered in a single colour on the vendor's brand colour so the
 * tiles read the same way in both themes. All tiles share one wide (2.5:1)
 * shape so they line up in lists: VMware's mark is a wordmark that would be
 * illegible in a square at row sizes, and the Proxmox mark sits centred in
 * the same tile. Both VMware products share the mark; the product name is
 * always written next to the icon where it matters.
 *
 * Trademarks belong to their owners (Broadcom Inc. / Proxmox Server Solutions
 * GmbH); they are shown only to identify which vendor a target connects to.
 */

interface ProviderIconProps {
  type: "vcenter" | "esxi" | "proxmox" | string;
  className?: string;
  /** Tile height in px; tiles are 2.5× as wide. */
  size?: number;
}

const VMWARE_PATH = "M.5 10.1a.505.505 0 00-.197.048.497.497 0 00-.25.68l1.138 2.475c.179.38.38.592.721.592.342 0 .542-.22.72-.592l1.003-2.186a.144.144 0 01.144-.092.16.16 0 01.157.16v2.118a.535.535 0 101.066 0v-1.73a.531.531 0 01.566-.552.52.52 0 01.541.551v1.73a.531.531 0 00.53.593.539.539 0 00.535-.592v-1.73a.531.531 0 01.564-.552.52.52 0 01.543.551v1.73a.531.531 0 00.528.593.535.535 0 00.535-.592v-1.969a1.234 1.234 0 00-1.283-1.23 1.647 1.647 0 00-1.14.486 1.26 1.26 0 00-1.095-.483 1.807 1.807 0 00-1.074.483 1.287 1.287 0 00-.961-.483 1.177 1.177 0 00-1.158.786l-.729 1.716-.933-2.203.011-.004A.505.505 0 00.5 10.1zm18.43.06a.27.27 0 00-.266.274h.002v3.142a.27.27 0 10.535 0v-1.222c0-1.037.571-1.56 1.27-1.643a.266.266 0 00.238-.274.258.258 0 00-.266-.269 1.465 1.465 0 00-1.242.88v-.614a.266.266 0 00-.271-.274zm-6.735.008a.273.273 0 00-.25.217l-.912 2.627-.902-2.62a.28.28 0 00-.274-.22.266.266 0 00-.27.258.493.493 0 00.034.144l1.09 3.037.02-.007a.319.319 0 00.298.242.3.3 0 00.293-.242l.903-2.583.896 2.583a.3.3 0 00.293.242h.018a.319.319 0 00.293-.242l1.097-3.038a.512.512 0 00.033-.144.258.258 0 00-.265-.25.262.262 0 00-.258.209l-.918 2.63-.904-2.626a.285.285 0 00-.278-.217h-.025a.273.273 0 00-.012 0zm10.168.008a1.75 1.75 0 00-1.691 1.851 1.765 1.765 0 001.76 1.858l-.008.013a1.784 1.784 0 001.33-.539.228.228 0 00.082-.17.228.228 0 00-.379-.168 1.435 1.435 0 01-1.018.415 1.237 1.237 0 01-1.24-1.207h2.555a.247.247 0 00.246-.247c0-.945-.593-1.806-1.637-1.806zm-5.744.002a1.571 1.571 0 00-.158.006 2.384 2.384 0 00-1.078.205.22.22 0 00-.143.222.24.24 0 00.235.229.266.266 0 00.095-.024 1.822 1.822 0 01.834-.162c.691 0 1.07.334 1.07.979v.125a3.796 3.796 0 00-1.103-.15c-.892 0-1.52.4-1.52 1.16l-.003-.004c0 .736.671 1.117 1.34 1.117a1.575 1.575 0 001.298-.62v.343a.247.247 0 00.254.25.254.254 0 00.258-.262v-1.983a1.416 1.416 0 00-.379-1.046 1.571 1.571 0 00-1-.385zm5.719.43c.714 0 1.085.565 1.139 1.214h-2.278a1.222 1.222 0 011.139-1.215zm-5.885 1.382a3.75 3.75 0 011.057.153V12.49c0 .57-.539.973-1.2.973-.485 0-.904-.261-.904-.713 0-.467.375-.76 1.047-.76Z";
const PROXMOX_PATH = "M4.928 1.825c-1.09.553-1.09.64-.07 1.78 5.655 6.295 7.004 7.782 7.107 7.782.139.017 7.971-8.542 8.058-8.801.034-.07-.208-.312-.519-.536-.415-.312-.864-.433-1.712-.467-1.59-.104-2.144.242-4.115 2.455-.899 1.003-1.66 1.833-1.66 1.833-.017 0-.76-.813-1.642-1.798S8.473 2.1 8.127 1.91c-.796-.45-2.421-.484-3.2-.086zM1.297 4.367C.45 4.695 0 5.007 0 5.248c0 .121 1.331 1.678 2.94 3.459 1.625 1.78 2.939 3.268 2.939 3.302 0 .035-1.331 1.522-2.94 3.303C1.314 17.11.017 18.683.035 18.822c.086.467 1.504 1.055 2.541 1.055 1.678-.018 2.058-.312 5.603-4.202 1.78-1.954 3.233-3.614 3.233-3.666 0-.069-1.435-1.694-3.199-3.63-2.3-2.508-3.423-3.632-3.96-3.874-.812-.398-2.126-.467-2.956-.138zm18.467.12c-.502.26-1.764 1.505-3.943 3.891-1.763 1.937-3.199 3.562-3.199 3.631 0 .07 1.453 1.712 3.234 3.666 3.544 3.89 3.925 4.184 5.602 4.202 1.038 0 2.455-.588 2.542-1.055.017-.156-1.28-1.712-2.905-3.493-1.608-1.78-2.94-3.285-2.94-3.32 0-.034 1.332-1.539 2.94-3.32C22.72 6.91 24.017 5.352 24 5.214c-.087-.45-1.366-.968-2.473-1.038-.795-.034-1.21.035-1.763.312zM7.954 16.973c-2.144 2.369-3.908 4.374-3.943 4.46-.034.07.208.312.52.537.414.311.864.432 1.711.467 1.574.103 2.161-.26 4.15-2.508.864-.968 1.608-1.78 1.625-1.78s.761.812 1.643 1.798c2.023 2.248 2.559 2.576 4.132 2.49.848-.035 1.297-.156 1.712-.467.311-.225.553-.467.519-.536-.087-.26-7.92-8.819-8.058-8.801-.069 0-1.867 1.954-4.011 4.34z";

// Brand colours (Simple Icons): VMware #607078, Proxmox #E57000.
const VMWARE_BG = "#607078";
const PROXMOX_BG = "#E57000";

// Every provider tile is this wide relative to its height, so icons line up
// in lists whatever the vendor.
const TILE_RATIO = 2.5;

function VMwareIcon({ size = 20, className, label }: { size?: number; className?: string; label: string }) {
  const width = Math.round(size * TILE_RATIO);
  const radius = Math.max(3, Math.round(size * 0.2));
  // The wordmark occupies y≈10.1–13.95 of its 24-unit box; crop to that band
  // and let it fill the tile's width with a little padding on each side.
  const pad = size * 0.16;
  return (
    <svg width={width} height={size} viewBox={`0 0 ${width} ${size}`} className={className} role="img" aria-label={label}>
      <title>{label}</title>
      <rect x="0" y="0" width={width} height={size} rx={radius} fill={VMWARE_BG} />
      <svg x={pad} y="0" width={width - pad * 2} height={size} viewBox="0 9.9 24 4.3" preserveAspectRatio="xMidYMid meet">
        <path d={VMWARE_PATH} fill="#fff" />
      </svg>
    </svg>
  );
}

function ProxmoxIcon({ size = 20, className }: { size?: number; className?: string }) {
  // Same wide tile as VMware so the two line up in lists; the square mark
  // sits centred in it.
  const width = Math.round(size * TILE_RATIO);
  const radius = Math.max(3, Math.round(size * 0.2));
  const pad = size * 0.16;
  const mark = size - pad * 2;
  return (
    <svg width={width} height={size} viewBox={`0 0 ${width} ${size}`} className={className} role="img" aria-label="Proxmox VE">
      <title>Proxmox VE</title>
      <rect x="0" y="0" width={width} height={size} rx={radius} fill={PROXMOX_BG} />
      <svg x={(width - mark) / 2} y={pad} width={mark} height={mark} viewBox="0 0 24 24">
        <path d={PROXMOX_PATH} fill="#fff" />
      </svg>
    </svg>
  );
}

export default function ProviderIcon({ type, className, size = 20 }: ProviderIconProps) {
  switch (type) {
    case "vcenter":
      return <VMwareIcon size={size} className={className} label="VMware vCenter" />;
    case "esxi":
      return <VMwareIcon size={size} className={className} label="VMware ESXi" />;
    case "vmware": // template platform, not a target type
      return <VMwareIcon size={size} className={className} label="VMware" />;
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
