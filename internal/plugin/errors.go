package plugin

import "errors"

// The contract's error taxonomy. Real adapters must return these sentinels
// (directly or wrapped with fmt.Errorf's %w, checkable with errors.Is) from
// the matching failure conditions so core packages and the conformance suite
// can branch on a stable, versioned set of outcomes instead of parsing error
// strings.
var (
	// ErrNotDetected is returned by any per-host operation (Capabilities,
	// Observe, Resolve, Compile, Verify, Launch) invoked against a
	// HostInstance the adapter's own Detect call did not return for this
	// invocation.
	ErrNotDetected = errors.New("plugin: host instance not detected")

	// ErrUnsupportedOperation is returned when the host has no
	// corresponding operation or concept at all: the adapter's
	// CapabilityManifest rates the requested concept/operation
	// CapabilityUnsupported (docs/knowledge/README.md §5 "UNSUPPORTED: the
	// host has no corresponding operation or concept").
	ErrUnsupportedOperation = errors.New("plugin: operation unsupported for this host")

	// ErrCapabilityDenied is returned when a write operation (Compile,
	// Launch) is requested for a concept whose declared ReconcileMode is
	// ReconcileBlocked: the operation exists in principle but this adapter
	// must not perform it for this host/concept right now.
	ErrCapabilityDenied = errors.New("plugin: capability denied for requested operation")

	// ErrContractVersionMismatch is returned by Registry.Register when a
	// manifest's ContractVersion major component does not match the major
	// version this build's registry expects (ADR 0005 "Contract versioning
	// policy").
	ErrContractVersionMismatch = errors.New("plugin: contract version mismatch")
)
