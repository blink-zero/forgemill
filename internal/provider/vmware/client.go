package vmware

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/session/cache"
	"github.com/vmware/govmomi/vim25/soap"

	"github.com/forgemill/forgemill/internal/provider"
)

func init() {
	// Register vCenter provider
	provider.RegisterProvider("vcenter", func(hostname string, port int, username, password string, validateCerts bool) provider.Provider {
		return New(hostname, port, username, password, validateCerts)
	})
	provider.RegisterMetadata("vcenter", &provider.ProviderMetadata{
		Name:        "vCenter",
		Description: "VMware vCenter Server — manages multiple ESXi hosts, supports full VM lifecycle including clones and templates.",
		Icon:        "vcenter",
		Defaults: provider.ProviderDefaults{
			Port:                443,
			Username:            "administrator@vsphere.local",
			NamePlaceholder:     "Production vCenter",
			HostnamePlaceholder: "vcenter.example.com",
		},
		Hints: map[string]string{
			"port":     "Default: 443 (HTTPS)",
			"username": "Usually administrator@vsphere.local or an SSO user",
		},
		Features: provider.ProviderFeatures{
			Folders:          true,
			Clusters:         true,
			DiskProvisioning: true,
			LinkedClones:     true,
		},
		DeployFields: []provider.DeployField{
			{Key: "datacenter", Label: "Datacenter", Resource: "datacenters"},
			{Key: "cluster", Label: "Cluster", Resource: "clusters"},
			{Key: "datastore", Label: "Datastore", Resource: "datastores"},
			{Key: "network", Label: "Network", Resource: "networks"},
			{Key: "folder", Label: "Folder", Resource: "folders", Placeholder: "Default folder"},
		},
	})

	// Register ESXi standalone provider
	provider.RegisterProvider("esxi", func(hostname string, port int, username, password string, validateCerts bool) provider.Provider {
		return NewESXi(hostname, port, username, password, validateCerts)
	})
	provider.RegisterMetadata("esxi", &provider.ProviderMetadata{
		Name:        "ESXi",
		Description: "Standalone ESXi host — connects directly to a single host. Some features like folder placement and native templates are not available without vCenter.",
		Icon:        "esxi",
		Defaults: provider.ProviderDefaults{
			Port:                443,
			Username:            "root",
			NamePlaceholder:     "ESXi Host 01",
			HostnamePlaceholder: "esxi01.example.com",
		},
		Hints: map[string]string{
			"port":     "Default: 443 (HTTPS)",
			"username": "Usually root for standalone ESXi hosts",
		},
		Features: provider.ProviderFeatures{
			Folders:          false,
			Clusters:         false,
			DiskProvisioning: true,
			LinkedClones:     false,
		},
		DeployFields: []provider.DeployField{
			{Key: "datastore", Label: "Datastore", Resource: "datastores"},
			{Key: "network", Label: "Network", Resource: "networks"},
		},
	})
}

// PV-V1: Removed ctx/cancel from struct. Context is passed per-call.
type Provider struct {
	hostname      string
	port          int
	username      string
	password      string
	validateCerts bool
	esxiMode      bool
	client        *govmomi.Client
	mu            sync.Mutex
}

func New(hostname string, port int, username, password string, validateCerts bool) *Provider {
	return &Provider{
		hostname:      hostname,
		port:          port,
		username:      username,
		password:      password,
		validateCerts: validateCerts,
	}
}

// NewESXi creates a provider configured for direct ESXi host connection.
func NewESXi(hostname string, port int, username, password string, validateCerts bool) *Provider {
	return &Provider{
		hostname:      hostname,
		port:          port,
		username:      username,
		password:      password,
		validateCerts: validateCerts,
		esxiMode:      true,
	}
}

// PV-V1/V3: Connect now accepts context and acquires the mutex to prevent races.
func (p *Provider) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.connectLocked(ctx)
}

// connectLocked performs the actual connection under the mutex.
func (p *Provider) connectLocked(ctx context.Context) error {
	u, err := soap.ParseURL(fmt.Sprintf("https://%s:%d/sdk", p.hostname, p.port))
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	u.User = url.UserPassword(p.username, p.password)

	if p.esxiMode {
		// ESXi direct connection — use NewClient (cache.Session panics with govmomi.Client on ESXi)
		client, err := govmomi.NewClient(ctx, u, !p.validateCerts)
		if err != nil {
			return fmt.Errorf("login to ESXi host: %w", err)
		}
		p.client = client
		return nil
	}

	// vCenter — use session cache for persistent sessions
	s := &cache.Session{
		URL:      u,
		Insecure: !p.validateCerts,
	}

	client := new(govmomi.Client)
	if err := s.Login(ctx, client, nil); err != nil {
		return fmt.Errorf("login to host: %w", err)
	}

	p.client = client
	return nil
}

func (p *Provider) Disconnect() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		err := p.client.Logout(context.Background())
		p.client = nil
		return err
	}
	return nil
}

func (p *Provider) TestConnection(ctx context.Context) error {
	tmp := New(p.hostname, p.port, p.username, p.password, p.validateCerts)
	if p.esxiMode {
		tmp.esxiMode = true
	}
	if err := tmp.Connect(ctx); err != nil {
		return err
	}
	defer tmp.Disconnect()
	return nil
}

// PV-V1/V2: getClient returns the client, auto-connecting and validating
// the session. Context is passed per-call instead of stored on the struct.
func (p *Provider) getClient(ctx context.Context) (*govmomi.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		if err := p.connectLocked(ctx); err != nil {
			return nil, err
		}
	}
	// PV-V2: Check if session is still active, reconnect if expired
	mgr := session.NewManager(p.client.Client)
	if _, err := mgr.UserSession(ctx); err != nil {
		slog.Info("vSphere session expired, reconnecting")
		p.client = nil
		if err := p.connectLocked(ctx); err != nil {
			return nil, err
		}
	}
	return p.client, nil
}

// defaultDatacenter returns the datacenter path to use.
// ESXi has a single implicit datacenter named "ha-datacenter".
func (p *Provider) defaultDatacenter() string {
	if p.esxiMode {
		return "ha-datacenter"
	}
	return ""
}
