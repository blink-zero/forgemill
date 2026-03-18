package factory

import "fmt"

// GenerateAutoinstall creates an autoinstall.yaml for Ubuntu installations.
// This is the backward-compatible wrapper that computes platform-specific
// parameters and delegates to the AutoinstallInstaller via the registry.
func GenerateAutoinstall(targetType string, sshPassword string, sshPublicKey string, buildIP string, netmask string, gateway string) (string, error) {
	inst, err := GetInstaller("autoinstall")
	if err != nil {
		return "", fmt.Errorf("get autoinstall installer: %w", err)
	}

	plat, err := GetPlatform(targetType)
	if err != nil {
		return "", fmt.Errorf("unsupported target type: %s", targetType)
	}

	hints := plat.InstallerHints()

	return inst.GenerateConfig(InstallerParams{
		TargetType:           targetType,
		OSFamily:             "debian",
		OSVersion:            "24.04",
		Username:             "forgemill",
		Password:             sshPassword,
		SSHPublicKey:         sshPublicKey,
		Hostname:             "forgemill-template",
		Timezone:             "UTC",
		Locale:               "en_US.UTF-8",
		Keyboard:             "us",
		ExtraPackages:        plat.ProvisionerPackages("ubuntu"),
		BuildIP:              buildIP,
		Netmask:              netmask,
		Gateway:              gateway,
		InterfaceName:        plat.InterfaceName(),
		CloudInitDatasources: hints.CloudInitDatasources,
		CloudInitExtraLines:  hints.CloudInitExtraLines,
		PlatformServices:     hints.PlatformServices,
	})
}
