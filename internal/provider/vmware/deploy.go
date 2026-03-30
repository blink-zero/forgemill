package vmware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/forgemill/forgemill/internal/provider"
)

func (p *Provider) DeployVM(ctx context.Context, spec *provider.DeploySpec) (*provider.DeployResult, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	finder := find.NewFinder(client.Client, true)

	dcName := spec.Datacenter
	if p.esxiMode && dcName == "" {
		dcName = "ha-datacenter"
	}

	dc, err := finder.Datacenter(ctx, dcName)
	if err != nil {
		return nil, fmt.Errorf("find datacenter %q: %w", dcName, err)
	}
	finder.SetDatacenter(dc)

	tmpl, err := finder.VirtualMachine(ctx, spec.TemplateName)
	if err != nil {
		return nil, fmt.Errorf("find template %q: %w", spec.TemplateName, err)
	}

	// PV-V8: Fail explicitly on folder not found instead of silently falling back
	var folder *object.Folder
	if spec.Folder != "" {
		folder, err = finder.Folder(ctx, spec.Folder)
		if err != nil {
			return nil, fmt.Errorf("find folder %q: %w", spec.Folder, err)
		}
	} else {
		folder, err = finder.DefaultFolder(ctx)
		if err != nil {
			return nil, fmt.Errorf("find default folder: %w", err)
		}
	}

	pool, err := p.findResourcePool(ctx, finder, spec.Cluster)
	if err != nil {
		return nil, fmt.Errorf("find resource pool: %w", err)
	}

	var datastoreRef *types.ManagedObjectReference
	if spec.Datastore != "" {
		ds, err := finder.Datastore(ctx, spec.Datastore)
		if err != nil {
			return nil, fmt.Errorf("find datastore %q: %w", spec.Datastore, err)
		}
		ref := ds.Reference()
		datastoreRef = &ref
	}

	// Resolve optional host placement (vCenter only).
	var hostRef *types.ManagedObjectReference
	if spec.Host != "" && !p.esxiMode {
		host, err := finder.HostSystem(ctx, spec.Host)
		if err != nil {
			return nil, fmt.Errorf("find host %q: %w", spec.Host, err)
		}
		ref := host.Reference()
		hostRef = &ref
	}

	cloneSpec := types.VirtualMachineCloneSpec{
		Location: types.VirtualMachineRelocateSpec{
			Pool:      poolRef(pool),
			Datastore: datastoreRef,
			Host:      hostRef,
		},
		PowerOn:  true,
		Template: false,
	}

	if spec.CPU > 0 || spec.MemoryMB > 0 {
		cloneSpec.Config = &types.VirtualMachineConfigSpec{}
		if spec.CPU > 0 {
			cloneSpec.Config.NumCPUs = int32(spec.CPU)
		}
		if spec.MemoryMB > 0 {
			cloneSpec.Config.MemoryMB = int64(spec.MemoryMB)
		}
	}

	// PV-V5: Separate customization from network backing change.
	needsCustomization := spec.Hostname != "" || spec.IPAddress != "" ||
		spec.DomainName != "" || len(spec.DNS) > 0 || spec.Network != ""
	if needsCustomization {
		cloneSpec.Customization = p.buildCustomization(spec)
	}

	// Retrieve template device info for NIC and disk changes
	var vmProps mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, tmpl.Reference(), []string{"config.hardware.device", "config.guestId", "config.hardware.numCPU", "config.hardware.memoryMB", "config.firmware"}, &vmProps); err != nil {
		return nil, fmt.Errorf("get template devices: %w", err)
	}

	// Save the template's original disk size before any mutations (clone path
	// modifies the device objects in vmProps for disk resize).
	var templateOriginalDiskKB int64
	if vmProps.Config != nil {
		for _, dev := range vmProps.Config.Hardware.Device {
			if d, ok := dev.(*types.VirtualDisk); ok {
				templateOriginalDiskKB = d.CapacityInKB
				break
			}
		}
	}

	if spec.Network != "" {
		net, err := finder.Network(ctx, spec.Network)
		if err != nil {
			return nil, fmt.Errorf("find network %q: %w", spec.Network, err)
		}
		backing, err := net.EthernetCardBackingInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("get network backing for %q: %w", spec.Network, err)
		}

		// PV-V4: Preserve the original NIC type from the template
		var nicDevice types.BaseVirtualDevice
		if vmProps.Config != nil {
			for _, dev := range vmProps.Config.Hardware.Device {
				if card, ok := dev.(types.BaseVirtualEthernetCard); ok {
					ethCard := card.GetVirtualEthernetCard()
					ethCard.Backing = backing
					nicDevice = dev
					break
				}
			}
		}
		if nicDevice == nil {
			return nil, fmt.Errorf("no network adapter found in template")
		}
		deviceChange := types.VirtualDeviceConfigSpec{
			Operation: types.VirtualDeviceConfigSpecOperationEdit,
			Device:    nicDevice,
		}
		if cloneSpec.Config == nil {
			cloneSpec.Config = &types.VirtualMachineConfigSpec{}
		}
		cloneSpec.Config.DeviceChange = append(cloneSpec.Config.DeviceChange, &deviceChange)
	}

	// PV-V6: Apply DiskGB resize during clone if specified
	if spec.DiskGB > 0 && vmProps.Config != nil {
		for _, dev := range vmProps.Config.Hardware.Device {
			disk, ok := dev.(*types.VirtualDisk)
			if !ok {
				continue
			}
			requestedKB := int64(spec.DiskGB) * 1024 * 1024
			if requestedKB > disk.CapacityInKB {
				disk.CapacityInKB = requestedKB
				if cloneSpec.Config == nil {
					cloneSpec.Config = &types.VirtualMachineConfigSpec{}
				}
				cloneSpec.Config.DeviceChange = append(cloneSpec.Config.DeviceChange,
					&types.VirtualDeviceConfigSpec{
						Operation: types.VirtualDeviceConfigSpecOperationEdit,
						Device:    disk,
					},
				)
			}
			break
		}
	}

	// Apply disk provisioning override if specified.
	if spec.DiskProvisioning != "" && vmProps.Config != nil {
		for _, dev := range vmProps.Config.Hardware.Device {
			disk, ok := dev.(*types.VirtualDisk)
			if !ok {
				continue
			}
			backing := &types.VirtualDiskFlatVer2BackingInfo{
				DiskMode: string(types.VirtualDiskModePersistent),
			}
			switch spec.DiskProvisioning {
			case "thin":
				backing.ThinProvisioned = types.NewBool(true)
				backing.EagerlyScrub = types.NewBool(false)
			case "thick":
				backing.ThinProvisioned = types.NewBool(false)
				backing.EagerlyScrub = types.NewBool(false)
			case "thick_eager_zero":
				backing.ThinProvisioned = types.NewBool(false)
				backing.EagerlyScrub = types.NewBool(true)
			}
			if datastoreRef != nil {
				cloneSpec.Location.Disk = append(cloneSpec.Location.Disk,
					types.VirtualMachineRelocateSpecDiskLocator{
						DiskId:       disk.Key,
						Datastore:    *datastoreRef,
						DiskBackingInfo: backing,
					},
				)
			}
			break
		}
	}

	// Inject cloud-init credentials and network config via guestinfo properties.
	// VMware datasource requires metadata to activate; userdata alone is ignored.
	// Network config is embedded in metadata under the "network" key, which is
	// how cloud-init's built-in VMware datasource (21.3+) reads network config.
	if spec.PasswordHash != "" || spec.UserDataOverride != "" {
		var userdata string
		if spec.UserDataOverride != "" {
			userdata = spec.UserDataOverride
		} else {
			userdata = buildCloudInitUserdata(spec.PasswordHash, spec.PlainPassword, spec.SSHPublicKey)
		}
		userdataB64 := base64.StdEncoding.EncodeToString([]byte(userdata))
		metadataJSON := buildCloudInitMetadata(spec)
		metadataB64 := base64.StdEncoding.EncodeToString(metadataJSON)
		if cloneSpec.Config == nil {
			cloneSpec.Config = &types.VirtualMachineConfigSpec{}
		}
		cloneSpec.Config.ExtraConfig = append(cloneSpec.Config.ExtraConfig,
			&types.OptionValue{Key: "guestinfo.metadata", Value: metadataB64},
			&types.OptionValue{Key: "guestinfo.metadata.encoding", Value: "base64"},
			&types.OptionValue{Key: "guestinfo.userdata", Value: userdataB64},
			&types.OptionValue{Key: "guestinfo.userdata.encoding", Value: "base64"},
		)
	}

	task, err := tmpl.Clone(ctx, folder, spec.VMName, cloneSpec)
	if err != nil {
		// Standalone ESXi does not support CloneVM_Task. Fall back to
		// copying the template VMDK and registering a new VM.
		if p.esxiMode && isNotSupportedError(err) {
			slog.Info("CloneVM not supported on standalone ESXi, using file copy fallback", "vm", spec.VMName)
			return p.esxiDeployFallback(ctx, spec, client.Client, dc, finder, folder, pool, &vmProps, templateOriginalDiskKB, vmProps.Config.Firmware)
		}
		return nil, fmt.Errorf("clone VM: %w", err)
	}

	// BUG-02: Extract the VM moref from the clone task result so that
	// subsequent operations (power, snapshot, status sync, delete) can
	// look up the VM. Without this, VMRef is empty for all VMware VMs.
	info, err := task.WaitForResult(ctx, nil)
	if err != nil {
		if p.esxiMode && isNotSupportedError(err) {
			slog.Info("CloneVM task failed on standalone ESXi, using file copy fallback", "vm", spec.VMName)
			return p.esxiDeployFallback(ctx, spec, client.Client, dc, finder, folder, pool, &vmProps, templateOriginalDiskKB, vmProps.Config.Firmware)
		}
		return nil, fmt.Errorf("clone task failed: %w", err)
	}
	vmID := ""
	if info.Result != nil {
		if ref, ok := info.Result.(types.ManagedObjectReference); ok {
			vmID = ref.Value
		}
	}

	return &provider.DeployResult{
		TaskID: task.Reference().Value,
		VMID:   vmID,
	}, nil
}

// buildCloudInitUserdata generates a cloud-config YAML for credential injection.
func buildCloudInitUserdata(passwordHash, plainPassword, sshPublicKey string) string {
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	b.WriteString("users:\n")
	b.WriteString("  - name: forgemill\n")
	b.WriteString("    lock_passwd: false\n")
	b.WriteString(fmt.Sprintf("    passwd: %s\n", passwordHash))
	b.WriteString("    shell: /bin/bash\n")
	b.WriteString("    sudo: ALL=(ALL) NOPASSWD:ALL\n")
	b.WriteString("    groups: sudo\n")
	if sshPublicKey != "" {
		b.WriteString("    ssh_authorized_keys:\n")
		b.WriteString(fmt.Sprintf("      - %s\n", sshPublicKey))
	}
	// Belt-and-suspenders: chpasswd.users reliably sets the password even
	// when the user already exists from the template build phase.
	b.WriteString("chpasswd:\n")
	b.WriteString("  expire: false\n")
	if plainPassword != "" {
		b.WriteString("  users:\n")
		b.WriteString("    - name: forgemill\n")
		b.WriteString(fmt.Sprintf("      password: %s\n", plainPassword))
		b.WriteString("      type: text\n")
	}
	b.WriteString("ssh_pwauth: true\n")
	return b.String()
}

func (p *Provider) GetDeployProgress(ctx context.Context, taskID string) (*provider.Progress, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	ref := types.ManagedObjectReference{Type: "Task", Value: taskID}
	var task mo.Task
	pc := property.DefaultCollector(client.Client)
	if err := pc.RetrieveOne(ctx, ref, []string{"info"}, &task); err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	// PV-X3: Map govmomi task states to canonical provider constants
	progress := &provider.Progress{}
	progress.Percent = int(task.Info.Progress)
	switch task.Info.State {
	case types.TaskInfoStateQueued:
		progress.State = provider.ProgressStateQueued
		progress.Message = "Queued"
	case types.TaskInfoStateRunning:
		progress.State = provider.ProgressStateRunning
		progress.Message = "Deploying VM..."
		if task.Info.Progress > 0 {
			progress.Message = fmt.Sprintf("Deploying VM... %d%%", task.Info.Progress)
		}
	case types.TaskInfoStateSuccess:
		progress.State = provider.ProgressStateSuccess
		progress.Percent = 100
		progress.Message = "Deployment completed"
	case types.TaskInfoStateError:
		progress.State = provider.ProgressStateError
		progress.Message = "Deployment failed"
		if task.Info.Error != nil {
			progress.Message = task.Info.Error.LocalizedMessage
		}
	}
	return progress, nil
}

func (p *Provider) findResourcePool(ctx context.Context, finder *find.Finder, cluster string) (*object.ResourcePool, error) {
	if p.esxiMode {
		pool, err := finder.ResourcePool(ctx, "*/Resources")
		if err != nil {
			return finder.DefaultResourcePool(ctx)
		}
		return pool, nil
	}
	if cluster != "" {
		cl, err := finder.ClusterComputeResource(ctx, cluster)
		if err != nil {
			return nil, err
		}
		return cl.ResourcePool(ctx)
	}
	return finder.DefaultResourcePool(ctx)
}

func poolRef(pool *object.ResourcePool) *types.ManagedObjectReference {
	ref := pool.Reference()
	return &ref
}

func (p *Provider) buildCustomization(spec *provider.DeploySpec) *types.CustomizationSpec {
	hostname := spec.Hostname
	if hostname == "" {
		hostname = spec.VMName
	}

	cs := &types.CustomizationSpec{
		GlobalIPSettings: types.CustomizationGlobalIPSettings{
			DnsServerList: spec.DNS,
		},
	}

	// PV-V7: Enhanced Windows Sysprep customization
	if spec.OSType == "windows" {
		cs.Identity = &types.CustomizationSysprep{
			GuiUnattended: types.CustomizationGuiUnattended{
				AutoLogon:      false,
				AutoLogonCount: 0,
				TimeZone:       85, // UTC
			},
			UserData: types.CustomizationUserData{
				ComputerName: &types.CustomizationFixedName{Name: hostname},
				FullName:     "Administrator",
				OrgName:      "Organization",
			},
			Identification: types.CustomizationIdentification{
				JoinWorkgroup: "WORKGROUP",
			},
		}
	} else {
		cs.Identity = &types.CustomizationLinuxPrep{
			HostName: &types.CustomizationFixedName{Name: hostname},
			Domain:   spec.DomainName,
		}
	}

	if spec.IPAddress != "" {
		cs.NicSettingMap = []types.CustomizationAdapterMapping{
			{
				Adapter: types.CustomizationIPSettings{
					Ip:         &types.CustomizationFixedIp{IpAddress: spec.IPAddress},
					SubnetMask: spec.Netmask,
					Gateway:    []string{spec.Gateway},
				},
			},
		}
	} else {
		cs.NicSettingMap = []types.CustomizationAdapterMapping{
			{
				Adapter: types.CustomizationIPSettings{
					Ip: &types.CustomizationDhcpIpGenerator{},
				},
			},
		}
	}

	return cs
}

// PV-V17: GetResources now discovers resources from ALL datacenters.
// PV-V18: ESXi mode skips cluster discovery.
// PV-X6: Includes resource pool discovery.
func (p *Provider) GetResources(ctx context.Context) (*provider.Resources, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}

	finder := find.NewFinder(client.Client, true)
	platform := "vcenter"
	if p.esxiMode {
		platform = "esxi"
	}
	resources := &provider.Resources{
		Datastores:    []provider.ResourceItem{},
		Networks:      []provider.ResourceItem{},
		Folders:       []provider.ResourceItem{},
		Clusters:      []provider.ResourceItem{},
		Datacenters:   []provider.ResourceItem{},
		ResourcePools: []provider.ResourceItem{},
		Hosts:         []provider.ResourceItem{},
		Platform:      platform,
		Defaults:      map[string]string{"disk_provisioning": "thin"},
	}

	dcs, err := finder.DatacenterList(ctx, "*")
	if err == nil {
		for _, dc := range dcs {
			resources.Datacenters = append(resources.Datacenters, provider.ResourceItem{
				Name: dc.Name(),
				ID:   dc.Reference().Value,
			})
		}
	}

	for _, dc := range dcs {
		finder.SetDatacenter(dc)
		prefix := ""
		if len(dcs) > 1 {
			prefix = dc.Name() + "/"
		}

		if dsList, err := finder.DatastoreList(ctx, "*"); err == nil {
			for _, ds := range dsList {
				resources.Datastores = append(resources.Datastores, provider.ResourceItem{
					Name: prefix + ds.Name(),
					ID:   ds.Reference().Value,
				})
			}
		}

		if nets, err := finder.NetworkList(ctx, "*"); err == nil {
			for _, n := range nets {
				resources.Networks = append(resources.Networks, provider.ResourceItem{
					Name: prefix + networkName(n.GetInventoryPath()),
					ID:   n.Reference().Value,
					Path: n.GetInventoryPath(),
				})
			}
		}

		if folders, err := finder.FolderList(ctx, "*"); err == nil {
			for _, f := range folders {
				resources.Folders = append(resources.Folders, provider.ResourceItem{
					Name: prefix + f.InventoryPath,
					ID:   f.Reference().Value,
				})
			}
		}

		if !p.esxiMode {
			if clusters, err := finder.ClusterComputeResourceList(ctx, "*"); err == nil {
				for _, c := range clusters {
					resources.Clusters = append(resources.Clusters, provider.ResourceItem{
						Name: prefix + c.Name(),
						ID:   c.Reference().Value,
					})
				}
			}
			if pools, err := finder.ResourcePoolList(ctx, "*"); err == nil {
				for _, rp := range pools {
					resources.ResourcePools = append(resources.ResourcePools, provider.ResourceItem{
						Name: prefix + rp.InventoryPath,
						ID:   rp.Reference().Value,
					})
				}
			}
			if hosts, err := finder.HostSystemList(ctx, "*"); err == nil {
				for _, h := range hosts {
					resources.Hosts = append(resources.Hosts, provider.ResourceItem{
						Name: prefix + h.Name(),
						ID:   h.Reference().Value,
					})
				}
			}
		}
	}

	// Populate smart defaults with first available resource of each type
	if len(resources.Datacenters) > 0 {
		resources.Defaults["datacenter"] = resources.Datacenters[0].Name
	}
	if len(resources.Datastores) > 0 {
		resources.Defaults["datastore"] = resources.Datastores[0].Name
	}
	if len(resources.Networks) > 0 {
		if resources.Networks[0].Path != "" {
			resources.Defaults["network"] = resources.Networks[0].Path
		} else {
			resources.Defaults["network"] = resources.Networks[0].Name
		}
	}

	return resources, nil
}

// esxiDeployFallback deploys a VM on standalone ESXi by copying the template's
// VMDK via CopyDatastoreFile_Task and registering a new VM with CreateVM_Task.
// This is used when CloneVM_Task is not supported (standalone ESXi without vCenter).
func (p *Provider) esxiDeployFallback(
	ctx context.Context,
	spec *provider.DeploySpec,
	vimClient *vim25.Client,
	dc *object.Datacenter,
	finder *find.Finder,
	folder *object.Folder,
	pool *object.ResourcePool,
	vmProps *mo.VirtualMachine,
	templateOriginalDiskKB int64,
	firmware string,
) (*provider.DeployResult, error) {
	if vmProps.Config == nil {
		return nil, fmt.Errorf("template has no configuration")
	}

	// Find the template's primary VMDK path and disk properties.
	var srcDiskPath string
	var diskSizeKB int64
	var thinProvisioned *bool
	var controllerKey int32
	for _, dev := range vmProps.Config.Hardware.Device {
		disk, ok := dev.(*types.VirtualDisk)
		if !ok {
			continue
		}
		backing, ok := disk.Backing.(*types.VirtualDiskFlatVer2BackingInfo)
		if !ok {
			continue
		}
		srcDiskPath = backing.FileName
		diskSizeKB = disk.CapacityInKB
		thinProvisioned = backing.ThinProvisioned
		controllerKey = disk.ControllerKey
		break
	}
	if srcDiskPath == "" {
		return nil, fmt.Errorf("no VMDK found in template")
	}

	// Override disk provisioning if specified.
	if spec.DiskProvisioning != "" {
		switch spec.DiskProvisioning {
		case "thin":
			thinProvisioned = types.NewBool(true)
		case "thick", "thick_eager_zero":
			thinProvisioned = types.NewBool(false)
		}
	}

	// Override disk size if requested and larger than template.
	if spec.DiskGB > 0 {
		requestedKB := int64(spec.DiskGB) * 1024 * 1024
		if requestedKB > diskSizeKB {
			diskSizeKB = requestedKB
		}
	}

	// Determine target datastore.
	dsName := parseDatastoreName(srcDiskPath)
	if spec.Datastore != "" {
		dsName = spec.Datastore
	}
	if dsName == "" {
		return nil, fmt.Errorf("cannot determine target datastore")
	}

	// Create a directory for the new VM on the datastore.
	fm := object.NewFileManager(vimClient)
	vmDir := fmt.Sprintf("[%s] %s", dsName, spec.VMName)
	_ = fm.MakeDirectory(ctx, vmDir, dc, true)

	// Copy the template VMDK to the new VM directory using VirtualDiskManager.
	// CopyDatastoreFile only copies the descriptor, not the flat extent.
	// CopyVirtualDisk properly handles multi-part VMDKs and can convert the
	// disk format during copy via destSpec — this is the only way to honour
	// a thin/thick provisioning request on standalone ESXi (the ThinProvisioned
	// flag on the backing descriptor is cosmetic; it does not convert the VMDK).
	dstDiskPath := fmt.Sprintf("[%s] %s/%s.vmdk", dsName, spec.VMName, spec.VMName)
	slog.Info("copying template VMDK for ESXi fallback deploy", "src", srcDiskPath, "dst", dstDiskPath, "disk_provisioning", spec.DiskProvisioning)
	vdm := object.NewVirtualDiskManager(vimClient)

	// Build a destSpec to control the disk format during copy.
	// CopyVirtualDisk with nil spec inflates thin VMDKs to thick on ESXi.
	// VirtualDiskSpec.AdapterType is required by ESXi (empty string causes task failure).
	// lsiLogic is the correct value for both lsiLogic and pvscsi SCSI controllers.
	diskType := types.VirtualDiskTypeThin
	switch spec.DiskProvisioning {
	case "thick":
		diskType = types.VirtualDiskTypePreallocated
	case "thick_eager_zero":
		diskType = types.VirtualDiskTypeEagerZeroedThick
	}
	copySpec := types.BaseVirtualDiskSpec(&types.VirtualDiskSpec{
		DiskType:    string(diskType),
		AdapterType: string(types.VirtualDiskAdapterTypeLsiLogic),
	})

	copyTask, err := vdm.CopyVirtualDisk(ctx, srcDiskPath, dc, dstDiskPath, dc, copySpec, false)
	var copyTaskResult *object.Task
	if err == nil {
		copyTaskResult = copyTask
		_, err = copyTask.WaitForResult(ctx, nil)
	}
	if err != nil {
		// If format conversion is rejected, fall back to plain copy (thick).
		// The ThinProvisioned flag in the VM descriptor will still be set correctly.
		slog.Warn("VMDK copy with format spec failed, retrying without spec", "error", err, "disk_provisioning", spec.DiskProvisioning)
		copyTask, err = vdm.CopyVirtualDisk(ctx, srcDiskPath, dc, dstDiskPath, dc, nil, false)
		if err != nil {
			return nil, fmt.Errorf("start VMDK copy: %w", err)
		}
		copyTaskResult = copyTask
		if _, err = copyTask.WaitForResult(ctx, nil); err != nil {
			return nil, fmt.Errorf("VMDK copy failed: %w", err)
		}
	}
	_ = copyTaskResult
	if err != nil {
		return nil, fmt.Errorf("start VMDK copy: %w", err)
	}

	// Extend the copied VMDK if the requested size is larger than the template.
	// CopyVirtualDisk preserves the original size; we must explicitly extend it.
	// NOTE: The vmProps device objects may have been mutated by Deploy()'s clone
	// path (which sets disk.CapacityInKB to the requested size). We saved the
	// original template capacity in originalDiskSizeKB before any overrides.
	slog.Info("ESXi fallback disk check", "spec_disk_gb", spec.DiskGB, "original_template_disk_kb", templateOriginalDiskKB, "original_template_disk_gb", templateOriginalDiskKB/1024/1024)
	if spec.DiskGB > 0 {
		requestedKB := int64(spec.DiskGB) * 1024 * 1024
		slog.Info("ESXi fallback disk extend check", "requested_kb", requestedKB, "original_template_kb", templateOriginalDiskKB, "needs_extend", requestedKB > templateOriginalDiskKB)
		if requestedKB > templateOriginalDiskKB {
			slog.Info("extending VMDK for ESXi fallback deploy", "dst", dstDiskPath, "size_gb", spec.DiskGB)
			extendTask, extErr := vdm.ExtendVirtualDisk(ctx, dstDiskPath, dc, requestedKB, types.NewBool(false))
			if extErr != nil {
				slog.Warn("failed to start VMDK extend", "error", extErr)
			} else if _, extErr = extendTask.WaitForResult(ctx, nil); extErr != nil {
				slog.Warn("VMDK extend failed", "error", extErr)
			}
		}
	}

	// Determine CPU and memory (use spec overrides or template values).
	numCPUs := vmProps.Config.Hardware.NumCPU
	if spec.CPU > 0 {
		numCPUs = int32(spec.CPU)
	}
	memoryMB := int64(vmProps.Config.Hardware.MemoryMB)
	if spec.MemoryMB > 0 {
		memoryMB = int64(spec.MemoryMB)
	}

	configSpec := types.VirtualMachineConfigSpec{
		Name:     spec.VMName,
		GuestId:  vmProps.Config.GuestId,
		NumCPUs:  numCPUs,
		MemoryMB: memoryMB,
		Firmware: firmware,
		Files: &types.VirtualMachineFileInfo{
			VmPathName: vmDir,
		},
	}

	// Add a SCSI controller matching the template.
	ctrlSpec := buildControllerFromTemplate(vmProps.Config.Hardware.Device, controllerKey)
	configSpec.DeviceChange = append(configSpec.DeviceChange, ctrlSpec)

	// Add disk pointing at the copied VMDK.
	configSpec.DeviceChange = append(configSpec.DeviceChange, &types.VirtualDeviceConfigSpec{
		Operation: types.VirtualDeviceConfigSpecOperationAdd,
		Device: &types.VirtualDisk{
			CapacityInKB: diskSizeKB,
			VirtualDevice: types.VirtualDevice{
				Key:           -2,
				ControllerKey: ctrlSpec.Device.GetVirtualDevice().Key,
				UnitNumber:    types.NewInt32(0),
				Backing: &types.VirtualDiskFlatVer2BackingInfo{
					DiskMode:        string(types.VirtualDiskModePersistent),
					ThinProvisioned: thinProvisioned,
					VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
						FileName: dstDiskPath,
					},
				},
			},
		},
	})

	// Add NIC preserving the template's adapter type.
	nicSpec, err := buildNICFromTemplate(ctx, finder, spec, vmProps.Config.Hardware.Device)
	if err != nil {
		slog.Warn("skipping NIC in ESXi fallback deploy", "error", err)
	} else if nicSpec != nil {
		configSpec.DeviceChange = append(configSpec.DeviceChange, nicSpec)
	}

	// Inject cloud-init credentials and network config via guestinfo.
	// VMware datasource requires metadata to activate; userdata alone is ignored.
	// Network config is embedded in metadata under the "network" key.
	if spec.PasswordHash != "" || spec.UserDataOverride != "" {
		var userdata string
		if spec.UserDataOverride != "" {
			userdata = spec.UserDataOverride
		} else {
			userdata = buildCloudInitUserdata(spec.PasswordHash, spec.PlainPassword, spec.SSHPublicKey)
		}
		userdataB64 := base64.StdEncoding.EncodeToString([]byte(userdata))
		metadataJSON := buildCloudInitMetadata(spec)
		metadataB64 := base64.StdEncoding.EncodeToString(metadataJSON)
		configSpec.ExtraConfig = append(configSpec.ExtraConfig,
			&types.OptionValue{Key: "guestinfo.metadata", Value: metadataB64},
			&types.OptionValue{Key: "guestinfo.metadata.encoding", Value: "base64"},
			&types.OptionValue{Key: "guestinfo.userdata", Value: userdataB64},
			&types.OptionValue{Key: "guestinfo.userdata.encoding", Value: "base64"},
		)
	}

	// Create the VM.
	slog.Info("registering new VM via ESXi fallback", "vm", spec.VMName)
	createTask, err := folder.CreateVM(ctx, configSpec, pool, nil)
	if err != nil {
		return nil, fmt.Errorf("create VM: %w", err)
	}
	createInfo, err := createTask.WaitForResult(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create VM task failed: %w", err)
	}

	vmID := ""
	var vmObj *object.VirtualMachine
	if createInfo.Result != nil {
		if ref, ok := createInfo.Result.(types.ManagedObjectReference); ok {
			vmID = ref.Value
			vmObj = object.NewVirtualMachine(vimClient, ref)
		}
	}

	// Power on the new VM.
	if vmObj != nil {
		if powerTask, err := vmObj.PowerOn(ctx); err != nil {
			slog.Warn("failed to start power on", "vm", spec.VMName, "error", err)
		} else if _, err := powerTask.WaitForResult(ctx, nil); err != nil {
			slog.Warn("power on task failed", "vm", spec.VMName, "error", err)
		}
	}

	return &provider.DeployResult{
		TaskID: createTask.Reference().Value,
		VMID:   vmID,
	}, nil
}

// buildControllerFromTemplate creates a SCSI controller device spec matching
// the controller used by the template's primary disk.
func buildControllerFromTemplate(devices object.VirtualDeviceList, controllerKey int32) *types.VirtualDeviceConfigSpec {
	spec := &types.VirtualDeviceConfigSpec{
		Operation: types.VirtualDeviceConfigSpecOperationAdd,
	}

	for _, dev := range devices {
		if dev.GetVirtualDevice().Key != controllerKey {
			continue
		}
		switch dev.(type) {
		case *types.ParaVirtualSCSIController:
			spec.Device = &types.ParaVirtualSCSIController{
				VirtualSCSIController: types.VirtualSCSIController{
					SharedBus: types.VirtualSCSISharingNoSharing,
					VirtualController: types.VirtualController{
						VirtualDevice: types.VirtualDevice{Key: -1},
						BusNumber:     0,
					},
				},
			}
			return spec
		case *types.VirtualLsiLogicController:
			spec.Device = &types.VirtualLsiLogicController{
				VirtualSCSIController: types.VirtualSCSIController{
					SharedBus: types.VirtualSCSISharingNoSharing,
					VirtualController: types.VirtualController{
						VirtualDevice: types.VirtualDevice{Key: -1},
						BusNumber:     0,
					},
				},
			}
			return spec
		}
		break
	}

	// Default: LSI Logic SAS (most common for modern ESXi).
	spec.Device = &types.VirtualLsiLogicSASController{
		VirtualSCSIController: types.VirtualSCSIController{
			SharedBus: types.VirtualSCSISharingNoSharing,
			VirtualController: types.VirtualController{
				VirtualDevice: types.VirtualDevice{Key: -1},
				BusNumber:     0,
			},
		},
	}
	return spec
}

// buildNICFromTemplate creates a NIC device spec matching the template's
// network adapter type, optionally overriding the network backing.
func buildNICFromTemplate(ctx context.Context, finder *find.Finder, spec *provider.DeploySpec, devices object.VirtualDeviceList) (*types.VirtualDeviceConfigSpec, error) {
	var templateNIC types.BaseVirtualDevice
	var backing types.BaseVirtualDeviceBackingInfo
	for _, dev := range devices {
		if card, ok := dev.(types.BaseVirtualEthernetCard); ok {
			templateNIC = dev
			backing = card.GetVirtualEthernetCard().Backing
			break
		}
	}
	if templateNIC == nil {
		return nil, nil
	}

	if spec.Network != "" {
		net, err := finder.Network(ctx, spec.Network)
		if err != nil {
			return nil, fmt.Errorf("find network %q: %w", spec.Network, err)
		}
		b, err := net.EthernetCardBackingInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("get network backing for %q: %w", spec.Network, err)
		}
		backing = b
	}

	base := types.VirtualEthernetCard{
		VirtualDevice: types.VirtualDevice{
			Key:     -3,
			Backing: backing,
		},
		AddressType: string(types.VirtualEthernetCardMacTypeGenerated),
	}

	var nic types.BaseVirtualDevice
	switch templateNIC.(type) {
	case *types.VirtualE1000:
		nic = &types.VirtualE1000{VirtualEthernetCard: base}
	case *types.VirtualE1000e:
		nic = &types.VirtualE1000e{VirtualEthernetCard: base}
	default:
		nic = &types.VirtualVmxnet3{
			VirtualVmxnet: types.VirtualVmxnet{VirtualEthernetCard: base},
		}
	}

	return &types.VirtualDeviceConfigSpec{
		Operation: types.VirtualDeviceConfigSpecOperationAdd,
		Device:    nic,
	}, nil
}

// isNotSupportedError checks whether an error indicates that the requested
// operation is not supported (common on standalone ESXi without vCenter).
func isNotSupportedError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not supported")
}

// parseDatastoreName extracts the datastore name from a VMware datastore path
// such as "[datastore1] vm/vm.vmdk".
func parseDatastoreName(dsPath string) string {
	start := strings.Index(dsPath, "[")
	end := strings.Index(dsPath, "]")
	if start >= 0 && end > start {
		return dsPath[start+1 : end]
	}
	return ""
}

// networkName extracts the display name from a network inventory path.
// e.g. "/ha-datacenter/network/VM Network" → "VM Network"
func networkName(inventoryPath string) string {
	if i := strings.LastIndex(inventoryPath, "/"); i >= 0 {
		return inventoryPath[i+1:]
	}
	return inventoryPath
}

// buildCloudInitMetadata builds the guestinfo.metadata JSON for cloud-init's
// VMware datasource. Includes instance identity and, when a static IP is
// specified, embeds network config v2 under the "network" key — the documented
// mechanism for the built-in VMware datasource (cloud-init 21.3+).
func buildCloudInitMetadata(spec *provider.DeploySpec) []byte {
	meta := map[string]interface{}{
		"instance-id":    spec.VMName,
		"local-hostname": spec.VMName,
	}

	// Build network config v2 (Netplan-compatible).
	// Uses a broad match pattern to handle varying VMware NIC names
	// (ens160, ens192, ens33, etc.).
	nic := map[string]interface{}{
		"match": map[string]string{"name": "e*"},
	}

	if spec.IPAddress != "" {
		cidr := netmaskToCIDRPrefix(spec.Netmask)
		nic["dhcp4"] = false
		nic["addresses"] = []string{fmt.Sprintf("%s/%s", spec.IPAddress, cidr)}
		if spec.Gateway != "" {
			nic["routes"] = []map[string]string{
				{"to": "default", "via": spec.Gateway},
			}
		}
		if len(spec.DNS) > 0 {
			nic["nameservers"] = map[string]interface{}{
				"addresses": spec.DNS,
			}
		}
	} else {
		nic["dhcp4"] = true
	}

	meta["network"] = map[string]interface{}{
		"version":   2,
		"ethernets": map[string]interface{}{"nics": nic},
	}

	data, err := json.Marshal(meta)
	if err != nil {
		// Fallback to minimal metadata without network config — should never happen.
		slog.Error("failed to marshal cloud-init metadata", "error", err)
		return []byte(fmt.Sprintf(`{"instance-id":"%s","local-hostname":"%s"}`, spec.VMName, spec.VMName))
	}
	return data
}

// netmaskToCIDRPrefix converts a dotted-decimal netmask (e.g. "255.255.255.0")
// to a CIDR prefix length string (e.g. "24"). Returns "24" as a safe default
// if the mask is empty or unparseable.
func netmaskToCIDRPrefix(mask string) string {
	if mask == "" {
		return "24"
	}
	ip := net.ParseIP(mask)
	if ip == nil {
		return "24"
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "24"
	}
	ones, _ := net.IPv4Mask(ip4[0], ip4[1], ip4[2], ip4[3]).Size()
	if ones == 0 {
		return "24"
	}
	return fmt.Sprintf("%d", ones)
}
