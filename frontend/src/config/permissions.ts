// Permission requirements shown in the target-creation form.
// Grouped by "category" so a non-expert admin can scan what each bucket covers.
// Only privileges backed by actual API calls in internal/provider/ are listed.

export interface PermissionGroup {
  category: string;
  description: string;
  privileges: string[];
  /** If true, only needed for the optional Template Factory feature. */
  optional?: boolean;
}

export interface ProviderPermissions {
  providerType: string;
  /** Short blurb shown at the top of the popover */
  summary: string;
  /** Link to official hypervisor docs about roles/privileges */
  docsUrl?: string;
  /** Common built-in role that already covers most of what we need, if any */
  bundledRole?: string;
  groups: PermissionGroup[];
  /** For admins who just want to paste into a role definition */
  copyAllLabel: string;
}

const VCENTER_PERMISSIONS: ProviderPermissions = {
  providerType: "vcenter",
  summary:
    "Create a dedicated vSphere role and apply it at the Datacenter (or higher) level. Inherited down to clusters, datastores, networks, and folders where Forgemill operates.",
  docsUrl: "https://docs.vmware.com/en/VMware-vSphere/8.0/vsphere-security/GUID-03B36057-B38C-479C-BD78-341CD83A0584.html",
  bundledRole: "Virtual Machine Power User + Datastore Consumer + Network Consumer — covers most of this. Create a custom role from those for tighter scope.",
  copyAllLabel: "Copy all privilege IDs",
  groups: [
    {
      category: "Datastore",
      description: "Space allocation for new VMs, template cloning, and disk expansion.",
      privileges: [
        "Datastore.AllocateSpace",
        "Datastore.Browse",
        "Datastore.FileManagement",
      ],
    },
    {
      category: "Network",
      description: "Assign VMs to port-groups / networks during deploy.",
      privileges: ["Network.Assign"],
    },
    {
      category: "Resource pool & folder",
      description: "Place cloned VMs into resource pools and folders.",
      privileges: [
        "Resource.AssignVMToPool",
        "Folder.Create",
      ],
    },
    {
      category: "VM configuration",
      description: "Deploy, resize CPU/memory, expand disks, inject cloud-init metadata via guestinfo.",
      privileges: [
        "VirtualMachine.Config.CPUCount",
        "VirtualMachine.Config.Memory",
        "VirtualMachine.Config.AddNewDisk",
        "VirtualMachine.Config.AddRemoveDevice",
        "VirtualMachine.Config.Advanced",
        "VirtualMachine.Config.DiskExtend",
      ],
    },
    {
      category: "VM lifecycle",
      description: "Power on/off, reset, suspend, access the VMRC console.",
      privileges: [
        "VirtualMachine.Interact.PowerOn",
        "VirtualMachine.Interact.PowerOff",
        "VirtualMachine.Interact.Reset",
        "VirtualMachine.Interact.Suspend",
        "VirtualMachine.Interact.ConsoleInteract",
      ],
    },
    {
      category: "Inventory",
      description: "Create / delete VM inventory entries.",
      privileges: [
        "VirtualMachine.Inventory.Create",
        "VirtualMachine.Inventory.Delete",
      ],
    },
    {
      category: "Provisioning (template clone)",
      description: "Clone from template and apply guest customisation specs.",
      privileges: [
        "VirtualMachine.Provisioning.Clone",
        "VirtualMachine.Provisioning.CustomizeGuest",
      ],
    },
    {
      category: "Snapshots",
      description: "Create, revert, and delete snapshots (with memory optional).",
      privileges: [
        "VirtualMachine.Snapshot.Create",
        "VirtualMachine.Snapshot.Remove",
        "VirtualMachine.Snapshot.Revert",
      ],
    },
    {
      category: "Template Factory",
      description: "Only needed if you build templates with the Factory (Packer). Read access to the content library / datastore for ISO uploads, plus mark-as-template.",
      optional: true,
      privileges: [
        "VirtualMachine.Provisioning.MarkAsTemplate",
        "VirtualMachine.Provisioning.MarkAsVM",
        "VirtualMachine.Provisioning.DeployTemplate",
      ],
    },
  ],
};

const ESXI_PERMISSIONS: ProviderPermissions = {
  providerType: "esxi",
  summary:
    "Standalone ESXi has a much flatter role model than vCenter. The built-in Administrator role works out of the box. For a dedicated service account, the below privileges cover every Forgemill operation. No vCenter-only features (Content Library, Guest Customisation Specs, native templates) apply here — templates are cloned via VMDK copy.",
  docsUrl: "https://docs.vmware.com/en/VMware-vSphere/8.0/vsphere-security/GUID-18071E9A-EED1-4968-8D51-E0B4F526FDA3.html",
  bundledRole: "Administrator (built-in) — works but is broader than needed. Prefer a custom role with the privileges below.",
  copyAllLabel: "Copy all privilege IDs",
  groups: [
    {
      category: "Datastore",
      description: "Disk space + file operations (template-VMDK copy on ESXi fallback).",
      privileges: [
        "Datastore.AllocateSpace",
        "Datastore.Browse",
        "Datastore.FileManagement",
      ],
    },
    {
      category: "Network",
      description: "Assign VMs to port-groups.",
      privileges: ["Network.Assign"],
    },
    {
      category: "VM configuration & lifecycle",
      description: "Deploy, resize, expand disks, power operations, console.",
      privileges: [
        "VirtualMachine.Config.AddNewDisk",
        "VirtualMachine.Config.AddRemoveDevice",
        "VirtualMachine.Config.Advanced",
        "VirtualMachine.Config.CPUCount",
        "VirtualMachine.Config.DiskExtend",
        "VirtualMachine.Config.Memory",
        "VirtualMachine.Interact.PowerOn",
        "VirtualMachine.Interact.PowerOff",
        "VirtualMachine.Interact.Reset",
        "VirtualMachine.Interact.Suspend",
        "VirtualMachine.Interact.ConsoleInteract",
      ],
    },
    {
      category: "Inventory",
      description: "Register and delete VMs.",
      privileges: [
        "VirtualMachine.Inventory.Create",
        "VirtualMachine.Inventory.Delete",
      ],
    },
    {
      category: "Snapshots",
      description: "Snapshot create / revert / delete.",
      privileges: [
        "VirtualMachine.Snapshot.Create",
        "VirtualMachine.Snapshot.Remove",
        "VirtualMachine.Snapshot.Revert",
      ],
    },
  ],
};

const PROXMOX_PERMISSIONS: ProviderPermissions = {
  providerType: "proxmox",
  summary:
    "Easiest: create a user in Proxmox, then assign the built-in PVEVMAdmin role on '/' (or just on /vms and /storage for a tighter scope). Alternatively, create a custom role with the privileges below. API-token auth (Datacenter → Permissions → API Tokens) is recommended over password auth.",
  docsUrl: "https://pve.proxmox.com/wiki/User_Management#pveum_roles",
  bundledRole: "PVEVMAdmin + PVEDatastoreUser + PVEAuditor — bundled roles that already include these. Grant on path / with propagate on.",
  copyAllLabel: "Copy all privilege IDs",
  groups: [
    {
      category: "VM audit & lifecycle",
      description: "Discover VMs across the cluster, start/stop/restart/suspend, monitor runtime state.",
      privileges: [
        "VM.Audit",
        "VM.PowerMgmt",
        "VM.Monitor",
        "Sys.Audit",
      ],
    },
    {
      category: "VM creation & cloning",
      description: "Allocate new VMIDs and clone from templates (full or linked).",
      privileges: [
        "VM.Allocate",
        "VM.Clone",
      ],
    },
    {
      category: "VM configuration",
      description: "Change CPU, memory, network bridge, attached CD/ISO.",
      privileges: [
        "VM.Config.CPU",
        "VM.Config.Memory",
        "VM.Config.Network",
        "VM.Config.CDROM",
        "VM.Config.Disk",
      ],
    },
    {
      category: "Datastore",
      description: "Read storage pools, allocate disk space, access templates.",
      privileges: [
        "Datastore.Audit",
        "Datastore.AllocateSpace",
        "Datastore.AllocateTemplate",
      ],
    },
    {
      category: "Console",
      description: "Access the noVNC console.",
      privileges: ["Sys.Console"],
    },
    {
      category: "Snippets (cloud-init)",
      description: "Only needed if using post-deploy actions merged into cloud-init on Proxmox — Forgemill uploads cloud-init snippets over SSH because the REST API doesn't cover snippet uploads. Grant SSH access on one Proxmox node for the same user.",
      optional: true,
      privileges: [
        "SSH access to one Proxmox node (not a PVE privilege — an OS-level login)",
      ],
    },
  ],
};

const PERMISSIONS_BY_TYPE: Record<string, ProviderPermissions> = {
  vcenter: VCENTER_PERMISSIONS,
  esxi: ESXI_PERMISSIONS,
  proxmox: PROXMOX_PERMISSIONS,
};

export function permissionsForType(type: string): ProviderPermissions | null {
  return PERMISSIONS_BY_TYPE[type] ?? null;
}

/** Flatten all privilege IDs for a provider into a single string, one per line. */
export function flattenPrivileges(p: ProviderPermissions, includeOptional = false): string {
  return p.groups
    .filter((g) => includeOptional || !g.optional)
    .flatMap((g) => g.privileges)
    .join("\n");
}
