package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/forgemill/forgemill/internal/db/models"
)

// ErrInvalidAddress is returned when an address override is neither an IP
// address nor a plausible hostname.
var ErrInvalidAddress = errors.New("address must be an IPv4/IPv6 address or a hostname")

// hostnameLabel is one RFC 1123 label: letters, digits and hyphens, not
// starting or ending with a hyphen.
var hostnameLabel = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// normalizeAddress trims and validates an operator-typed address. "" means
// clear. It accepts an IP (v4 or v6) or a hostname/FQDN; anything with a
// scheme, port, path or whitespace is refused so what's stored is exactly
// what ssh will be pointed at.
func normalizeAddress(raw string) (string, error) {
	a := strings.TrimSpace(raw)
	if a == "" {
		return "", nil
	}
	if len(a) > 253 {
		return "", ErrInvalidAddress
	}
	if ip := net.ParseIP(a); ip != nil {
		return ip.String(), nil
	}
	a = strings.TrimSuffix(a, ".")
	for _, label := range strings.Split(a, ".") {
		if !hostnameLabel.MatchString(label) {
			return "", ErrInvalidAddress
		}
	}
	return strings.ToLower(a), nil
}

// SetAddressOverride stores the address Forgemill should use to reach a VM
// when the hypervisor can't tell us (no guest agent), or clears it with "".
// It returns the VM as it now stands.
func (s *VMService) SetAddressOverride(ctx context.Context, vmID int64, address, actorName string) (*models.ManagedVM, error) {
	addr, err := normalizeAddress(address)
	if err != nil {
		return nil, err
	}
	vm, err := s.db.GetManagedVM(vmID)
	if err != nil {
		return nil, ErrVMNotFound
	}
	if vm.AddressOverride == addr {
		return vm, nil
	}
	if err := s.db.SetManagedVMAddressOverride(vm.ID, addr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrVMNotFound
		}
		return nil, fmt.Errorf("set address override: %w", err)
	}
	switch {
	case addr == "":
		s.recordVMEvent(vm, "info", fmt.Sprintf("Manual address cleared by %s; using the hypervisor-reported address", actorName))
	case vm.AddressOverride == "":
		s.recordVMEvent(vm, "info", fmt.Sprintf("Address set manually to %s by %s", addr, actorName))
	default:
		s.recordVMEvent(vm, "info", fmt.Sprintf("Manual address changed from %s to %s by %s", vm.AddressOverride, addr, actorName))
	}
	vm.AddressOverride = addr
	return vm, nil
}
