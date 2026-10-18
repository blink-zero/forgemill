import type { ManagedVM } from "@/types";

/** The address Forgemill connects to: the operator's manual address when set, else what the hypervisor reported. */
export function vmAddress(vm: Pick<ManagedVM, "ip_address" | "address_override">): string {
  return vm.address_override || vm.ip_address || "";
}

export function vmIsOn(powerState: string): boolean {
  return powerState === "poweredOn" || powerState === "running";
}

/** Why a VM can have no address, and the two ways out. Kept in one place so every surface says the same thing. */
export const NO_ADDRESS_EXPLANATION =
  "Hypervisors only report a guest's address through the guest agent (QEMU guest agent on Proxmox, VMware Tools on ESXi/vCenter). " +
  "Without one, Forgemill can't reach the VM to test credentials or run actions.";

export function guestAgentHint(targetType?: string): string {
  switch (targetType) {
    case "proxmox":
      return "Install qemu-guest-agent in the guest, enable “QEMU Guest Agent” under the VM's Options in Proxmox, then Sync here.";
    case "esxi":
    case "vcenter":
      return "Install VMware Tools or open-vm-tools in the guest, then Sync here.";
    default:
      return "Install the guest agent in the guest, then Sync here.";
  }
}
