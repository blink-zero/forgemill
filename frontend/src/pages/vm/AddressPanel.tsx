import { useState } from "react";
import { vms as vmApi } from "@/api/client";
import type { ManagedVM } from "@/types";
import { useToast } from "@/components/ui/toast";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Network, AlertTriangle, Loader2, X } from "lucide-react";
import { getErrorMessage } from "@/lib/utils";
import { NO_ADDRESS_EXPLANATION, guestAgentHint, vmAddress, vmIsOn } from "@/lib/vmAddress";

/*
  Where Forgemill reaches a VM when the hypervisor can't say. Two pieces:

  AddressCallout — the warning shown on a powered-on VM Forgemill has no
  address for. It names the cause (no guest agent) and both fixes, and opens
  the editor. Adopted VMs without an agent hit this on their first action.

  AddressEditor — the inline form that stores or clears the manual address.
*/

export function AddressCallout({ targetType, onEdit }: { targetType?: string; onEdit: () => void }) {
  return (
    <div className="rounded-md border border-warning/30 bg-warning/5 px-3 py-2.5 flex items-start gap-2.5" role="status">
      <AlertTriangle className="h-4 w-4 text-warning shrink-0 mt-0.5" />
      <div className="text-sm space-y-1 min-w-0 flex-1">
        <p className="font-medium">Forgemill doesn't know this VM's address</p>
        <p className="text-13 text-muted-foreground">{NO_ADDRESS_EXPLANATION}</p>
        <p className="text-13 text-muted-foreground">
          <span className="text-foreground">Fix either way:</span> {guestAgentHint(targetType)} Or, if the VM has a fixed address you already know, set it here and Forgemill will use that instead.
        </p>
        <div className="pt-1">
          <Button size="sm" variant="outline" className="gap-1.5" onClick={onEdit}>
            <Network className="h-3.5 w-3.5" /> Set address manually
          </Button>
        </div>
      </div>
    </div>
  );
}

export function AddressEditor({ vm, onSaved, onClose }: { vm: ManagedVM; onSaved: (vm: ManagedVM) => void; onClose: () => void }) {
  const { toast } = useToast();
  const [draft, setDraft] = useState(vm.address_override || "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const hasOverride = !!vm.address_override;
  const reported = vm.ip_address;

  const submit = async (address: string) => {
    setSaving(true);
    setError("");
    try {
      const res = await vmApi.setAddress(vm.id, address);
      onSaved(res.data);
      toast(address ? `Address set to ${res.data.address_override} — actions and credential checks will use it` : "Manual address cleared — using the hypervisor-reported address");
      onClose();
    } catch (e) {
      setError(getErrorMessage(e, "Failed to set address"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <form
      className="rounded-md border bg-muted/30 px-3 py-3 space-y-2.5"
      onSubmit={(e) => { e.preventDefault(); void submit(draft.trim()); }}
      aria-label="Set VM address"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="text-sm space-y-0.5 min-w-0">
          <p className="font-medium">Set the VM's address manually</p>
          <p className="text-13 text-muted-foreground">
            Used for SSH actions and credential checks instead of the hypervisor-reported address. {NO_ADDRESS_EXPLANATION} A manual address is kept across syncs until you clear it.
          </p>
        </div>
        <button type="button" onClick={onClose} className="text-muted-foreground hover:text-foreground shrink-0" aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
      <div className="grid gap-2 sm:grid-cols-[1fr_auto] sm:items-end">
        <div>
          <Label htmlFor="vm-address" className="text-xs">IP address or hostname</Label>
          <Input
            id="vm-address"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="10.0.0.5 or web-01.lab.internal"
            autoFocus
            spellCheck={false}
            autoComplete="off"
            className="font-mono"
          />
        </div>
        <div className="flex gap-1.5 flex-wrap">
          <Button type="submit" size="sm" disabled={saving || !draft.trim() || draft.trim() === vm.address_override}>
            {saving && <Loader2 className="h-3 w-3 mr-1 animate-spin" />} Save address
          </Button>
          {hasOverride && (
            <Button type="button" size="sm" variant="ghost" disabled={saving} onClick={() => void submit("")} title="Go back to the hypervisor-reported address">
              Clear
            </Button>
          )}
        </div>
      </div>
      <p className="text-2xs text-muted-foreground">
        Hypervisor reports: <span className="font-mono text-foreground/80">{reported || "no address"}</span>
        {!reported && vmIsOn(vm.power_state) && " — usually because the guest agent isn't installed"}
        {hasOverride && <> · currently using <span className="font-mono text-foreground/80">{vmAddress(vm)}</span> (manual)</>}
      </p>
      {error && (
        <p className="text-xs text-destructive flex items-center gap-1.5"><AlertTriangle className="h-3.5 w-3.5" /> {error}</p>
      )}
    </form>
  );
}
