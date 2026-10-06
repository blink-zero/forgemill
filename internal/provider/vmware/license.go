package vmware

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/forgemill/forgemill/internal/provider"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// HostCapabilities answers "will this host accept writes from us?".
//
// On a standalone ESXi host three checks each can say no and none can
// override a no: the license is the free vSphere Hypervisor SKU; the
// license is an evaluation whose expiration has passed (ESXi keeps
// reporting "Evaluation Mode" but refuses every write from then on); a
// real write probe — make and remove an empty directory on a datastore —
// answers RestrictedVersion, which is the ground truth whatever the
// license strings say.
//
// A vCenter target is never marked: its license list is the whole
// inventory's, including vCenter's own built-in evaluation entry, which
// reads as expired forever once real keys are in use, and the operations
// Forgemill needs are licensed per host, not per API session. Only the
// edition name is reported, for information.
//
// Errors reading the license are returned so the caller keeps the target's
// previous answer.
func (p *Provider) HostCapabilities(ctx context.Context) (*provider.HostCapabilities, error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return nil, err
	}
	var lm mo.LicenseManager
	if err := property.DefaultCollector(client.Client).RetrieveOne(ctx, *client.ServiceContent.LicenseManager, []string{"licenses", "evaluation"}, &lm); err != nil {
		return nil, fmt.Errorf("read license: %w", err)
	}
	caps := decideCapabilities(p.esxiMode, lm.Licenses, lm.Evaluation.Properties, time.Now())
	if p.esxiMode && caps.WritesAllowed {
		if refused, err := p.probeWrite(ctx); err != nil {
			slog.Warn("capability probe could not run; trusting the license", "host", p.hostname, "error", err)
		} else if refused {
			caps.WritesAllowed = false
			caps.Note = provider.WriteProbeRefusedMessage(caps.LicenseEdition)
		}
	}
	return caps, nil
}

// decideCapabilities is the license half of HostCapabilities, separated so
// it can be tested with license lists vcsim cannot produce.
func decideCapabilities(esxiMode bool, licenses []types.LicenseManagerLicenseInfo, evalProps []types.KeyAnyValue, now time.Time) *provider.HostCapabilities {
	caps := &provider.HostCapabilities{WritesAllowed: true}
	if !esxiMode {
		// Prefer a real key's name over the ever-present evaluation entry.
		for _, info := range licenses {
			if !isEvalLicense(info) {
				caps.LicenseEdition = info.Name
				break
			}
		}
		if caps.LicenseEdition == "" && len(licenses) > 0 {
			caps.LicenseEdition = licenses[0].Name
		}
		return caps
	}
	for _, info := range licenses {
		if caps.LicenseEdition == "" || isFreeLicense(info) {
			caps.LicenseEdition = info.Name
		}
		if isFreeLicense(info) {
			caps.WritesAllowed = false
			caps.Note = provider.FreeLicenseMessage
		}
		if caps.WritesAllowed && isEvalLicense(info) {
			props := append(append([]types.KeyAnyValue{}, info.Properties...), evalProps...)
			if expired, known := evalExpired(props, now); known && expired {
				caps.LicenseEdition = info.Name + " (expired)"
				caps.WritesAllowed = false
				caps.Note = provider.EvaluationExpiredMessage
			}
		}
	}
	return caps
}

// isFreeLicense recognises the free vSphere Hypervisor SKU. VMware has named
// it "VMware vSphere Hypervisor" / "VMware vSphere N Hypervisor" and keyed it
// with "free"/"hypervisor" editions across releases; paid editions are
// Standard / Enterprise Plus / Essentials and never say "Hypervisor".
func isFreeLicense(info types.LicenseManagerLicenseInfo) bool {
	edition := strings.ToLower(info.EditionKey)
	name := strings.ToLower(info.Name)
	switch {
	case strings.Contains(edition, "free"), strings.Contains(edition, "hypervisor"):
		return true
	case strings.Contains(name, "hypervisor") && !strings.Contains(name, "evaluation"):
		return true
	}
	return false
}

func isEvalLicense(info types.LicenseManagerLicenseInfo) bool {
	return strings.EqualFold(info.EditionKey, "eval") || strings.Contains(strings.ToLower(info.Name), "evaluation")
}

// evalExpired reads the evaluation's expiration from license properties
// (expirationHours / expirationMinutes as numbers, expirationDate as a time
// or RFC3339 string). known is false when no such property is present.
func evalExpired(props []types.KeyAnyValue, now time.Time) (expired, known bool) {
	for _, kv := range props {
		switch strings.ToLower(kv.Key) {
		case "expirationhours", "expirationminutes":
			if n, ok := asNumber(kv.Value); ok {
				known = true
				if n <= 0 {
					return true, true
				}
			}
		case "expirationdate":
			switch v := kv.Value.(type) {
			case time.Time:
				known = true
				if !v.IsZero() && v.Before(now) {
					return true, true
				}
			case string:
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					known = true
					if t.Before(now) {
						return true, true
					}
				}
			}
		}
	}
	return false, known
}

func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

// probeWrite makes and removes an empty directory on the first datastore.
// refused is true when the host answered with the license fault; any other
// failure is returned as err and decides nothing.
func (p *Provider) probeWrite(ctx context.Context) (refused bool, err error) {
	client, err := p.getClient(ctx)
	if err != nil {
		return false, err
	}
	finder := find.NewFinder(client.Client, true)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		return false, fmt.Errorf("datacenter: %w", err)
	}
	finder.SetDatacenter(dc)
	dss, err := finder.DatastoreList(ctx, "*")
	if err != nil || len(dss) == 0 {
		return false, fmt.Errorf("no datastore to probe: %v", err)
	}
	path := fmt.Sprintf("[%s] .forgemill-probe-%d", dss[0].Name(), time.Now().UnixNano())
	fm := object.NewFileManager(client.Client)
	if err := fm.MakeDirectory(ctx, path, dc, false); err != nil {
		if provider.IsLicenseRestricted(err) {
			return true, nil
		}
		return false, fmt.Errorf("make directory: %w", err)
	}
	if task, err := fm.DeleteDatastoreFile(ctx, path, dc); err == nil {
		if werr := task.Wait(ctx); werr != nil {
			slog.Warn("capability probe: could not remove probe directory", "path", path, "error", werr)
		}
	} else {
		slog.Warn("capability probe: could not remove probe directory", "path", path, "error", err)
	}
	return false, nil
}
