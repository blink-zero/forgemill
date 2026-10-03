package proxmox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/forgemill/forgemill/internal/provider"
)

// diskBusPrefixes is the order disks are enumerated in: it defines the
// stable Key (0, 1, 2 …) that ListDisks, ExpandDisk and AddDisk share.
var diskBusPrefixes = []string{"scsi", "virtio", "ide", "sata"}

const maxDiskSlots = 30

// parseDiskValue splits a Proxmox disk value — "local-lvm:vm-100-disk-0,size=32G,format=raw"
// — into its volume, storage, size and format. A cloud-init drive
// ("local:vm-100-cloudinit,media=cdrom") is still a disk slot but reports
// size 0.
func parseDiskValue(val string) (volume, storage string, sizeGB int, format string) {
	volume, rest, _ := strings.Cut(val, ",")
	storage, _, _ = strings.Cut(volume, ":")
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		if v, ok := strings.CutPrefix(part, "size="); ok {
			v = strings.TrimRight(v, "GgMmTt")
			if n, err := strconv.Atoi(v); err == nil {
				sizeGB = n
			}
		}
		if v, ok := strings.CutPrefix(part, "format="); ok {
			format = v
		}
	}
	return volume, storage, sizeGB, format
}

// enumerateDisks lists the disk slots present in a VM config in
// diskBusPrefixes order, assigning the sequential Key the other disk
// operations address them by.
func enumerateDisks(config qemuConfig) []provider.Disk {
	var disks []provider.Disk
	idx := 0
	for _, prefix := range diskBusPrefixes {
		for i := 0; i < maxDiskSlots; i++ {
			key := fmt.Sprintf("%s%d", prefix, i)
			if !config.Has(key) {
				continue
			}
			volume, storage, sizeGB, format := parseDiskValue(config.Str(key))
			disks = append(disks, provider.Disk{
				Key:          idx,
				Label:        key,
				SizeGB:       sizeGB,
				Datastore:    storage,
				Provisioning: format,
				Backing:      volume,
			})
			idx++
		}
	}
	return disks
}

// nextFreeDiskSlot returns the lowest unused scsiN slot.
func nextFreeDiskSlot(config qemuConfig) (string, error) {
	for i := 0; i < maxDiskSlots; i++ {
		key := fmt.Sprintf("scsi%d", i)
		if !config.Has(key) {
			return key, nil
		}
	}
	return "", fmt.Errorf("no free SCSI slot (scsi0-scsi%d all in use)", maxDiskSlots-1)
}

// storageExists checks the cluster storage list for a storage by name — the
// same list GetResources offers as datastores.
func (p *Provider) storageExists(ctx context.Context, name string) (bool, error) {
	body, err := p.doGet(ctx, "/storage")
	if err != nil {
		return false, err
	}
	var result struct {
		Data []struct {
			Storage string `json:"storage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}
	for _, s := range result.Data {
		if s.Storage == name {
			return true, nil
		}
	}
	return false, nil
}

// AddDisk allocates a new volume of spec.SizeGB on the storage and attaches
// it as the next free scsiN. Proxmox hot-plugs it when the VM is running and
// its hotplug setting includes "disk" (the default); otherwise the change is
// pending until the next power cycle — reported, never forced. Provisioning
// is not a per-disk choice on Proxmox (the storage type decides), so the
// service rejects a request that asks for one before it gets here.
func (p *Provider) AddDisk(ctx context.Context, vmID string, spec provider.DiskSpec) (*provider.Disk, error) {
	if spec.SizeGB <= 0 {
		return nil, fmt.Errorf("size_gb must be positive")
	}
	node := p.nodeFor(ctx, vmID)

	config, err := p.getVMConfig(ctx, node, vmID)
	if err != nil {
		return nil, err
	}
	key, err := nextFreeDiskSlot(config)
	if err != nil {
		return nil, err
	}

	storage := strings.TrimSpace(spec.Datastore)
	if storage == "" {
		// Same storage as the VM's first disk.
		for _, d := range enumerateDisks(config) {
			if d.SizeGB > 0 && d.Datastore != "" {
				storage = d.Datastore
				break
			}
		}
		if storage == "" {
			return nil, fmt.Errorf("storage is required: the VM has no existing disk to infer it from")
		}
	} else if known, err := p.storageExists(ctx, storage); err == nil && !known {
		return nil, fmt.Errorf("%w: storage %q", provider.ErrDatastoreNotFound, storage)
	}

	// "<storage>:<size>" asks Proxmox to allocate a fresh volume of that many GiB.
	data := url.Values{key: {fmt.Sprintf("%s:%d", storage, spec.SizeGB)}}
	configPath := fmt.Sprintf("/nodes/%s/qemu/%s/config", url.PathEscape(node), url.PathEscape(vmID))
	if err := p.doPut(ctx, configPath, data); err != nil {
		return nil, fmt.Errorf("add disk %s: %w", key, err)
	}

	pending := p.changePending(ctx, node, vmID, key)
	disk := &provider.Disk{Label: key, SizeGB: spec.SizeGB, Datastore: storage, Pending: pending}
	// Re-read to pick up the volume Proxmox allocated and the disk's Key in
	// the enumeration the other disk operations use.
	if after, err := p.getVMConfig(ctx, node, vmID); err == nil {
		for _, d := range enumerateDisks(after) {
			if d.Label == key {
				d.Pending = pending
				if d.SizeGB == 0 {
					d.SizeGB = spec.SizeGB
				}
				return &d, nil
			}
		}
	} else {
		provider.Warnf(ctx, "Disk added but the post-add config read failed", "vmID", vmID, "error", err)
	}
	return disk, nil
}
