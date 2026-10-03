package vmware

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vmware/govmomi/task"
	"github.com/vmware/govmomi/vim25/types"
)

func TestIsNotSupportedErrorMatchesTypedFaultEvenWhenWrapped(t *testing.T) {
	// What a CloneVM task reports on standalone ESXi, wrapped the way the
	// deploy path wraps it before the check.
	taskErr := task.Error{LocalizedMethodFault: &types.LocalizedMethodFault{
		Fault:            &types.NotSupported{},
		LocalizedMessage: "The operation is not supported on the object.",
	}}
	if !isNotSupportedError(fmt.Errorf("clone task failed: %w", taskErr)) {
		t.Error("typed NotSupported fault should be detected through the wrap")
	}
	// A different typed fault must not trigger the ESXi fallback.
	other := task.Error{LocalizedMethodFault: &types.LocalizedMethodFault{Fault: &types.InsufficientResourcesFault{}, LocalizedMessage: "out of resources"}}
	if isNotSupportedError(fmt.Errorf("clone task failed: %w", other)) {
		t.Error("InsufficientResourcesFault must not be treated as NotSupported")
	}
}

func TestIsNotSupportedErrorKeepsTextFallbackAndRejectsOthers(t *testing.T) {
	if !isNotSupportedError(errors.New("ServerFaultCode: The operation is Not Supported on the object")) {
		t.Error("text fallback must still match (ESXi fallback parity)")
	}
	if isNotSupportedError(errors.New("insufficient disk space")) {
		t.Error("unrelated error must not trigger the ESXi fallback")
	}
	if isNotSupportedError(nil) {
		t.Error("nil is not an error")
	}
}
