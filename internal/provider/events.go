package provider

import (
	"context"
	"fmt"
	"log/slog"
)

// EventSink receives operator-relevant events a provider raises while it
// works on a VM — the things that used to be slog.Warn only. The service
// attaches one to the context (deployment log, per-VM event list) so what
// the hypervisor did or refused is visible in the UI, not just in the
// server log.
type EventSink interface {
	Event(level, message string)
}

type eventsKey struct{}

// WithEvents returns a context that routes Infof/Warnf/Errorf to sink.
func WithEvents(ctx context.Context, sink EventSink) context.Context {
	return context.WithValue(ctx, eventsKey{}, sink)
}

func emit(ctx context.Context, level string, msg string, kv ...any) {
	switch level {
	case "warn":
		slog.Warn(msg, kv...)
	case "error":
		slog.Error(msg, kv...)
	default:
		slog.Info(msg, kv...)
	}
	if ctx == nil {
		return
	}
	if sink, ok := ctx.Value(eventsKey{}).(EventSink); ok && sink != nil {
		sink.Event(level, msg+kvSuffix(kv))
	}
}

// kvSuffix renders slog-style key/value pairs for the human-readable event
// ("… (vmid=101, error=…)"), skipping the keys that would just repeat the
// surrounding context.
func kvSuffix(kv []any) string {
	if len(kv) == 0 {
		return ""
	}
	out := ""
	for i := 0; i+1 < len(kv); i += 2 {
		k := fmt.Sprint(kv[i])
		if k == "vmID" || k == "vmid" || k == "vm" {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%s=%v", k, kv[i+1])
	}
	if out == "" {
		return ""
	}
	return " (" + out + ")"
}

// Infof / Warnf / Errorf log like slog and forward to the context's sink.
func Infof(ctx context.Context, msg string, kv ...any)  { emit(ctx, "info", msg, kv...) }
func Warnf(ctx context.Context, msg string, kv ...any)  { emit(ctx, "warn", msg, kv...) }
func Errorf(ctx context.Context, msg string, kv ...any) { emit(ctx, "error", msg, kv...) }

// PartialDeployError reports that DeployVM created the VM but could not
// finish shaping it (disk resize, power-on, …). The service registers the
// VM so it can be fixed or destroyed, and fails the deployment with Err.
type PartialDeployError struct {
	VMID string
	Err  error
}

func (e *PartialDeployError) Error() string { return e.Err.Error() }
func (e *PartialDeployError) Unwrap() error { return e.Err }
