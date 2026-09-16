package runtime

import (
	"fmt"
	hostcontext "github.com/wangzitian0/oh-my-code-agent/internal/context"
	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// validateCompiledHost checks the immutable compile target, never a mutable
// CurrentRecord. Exact-version equality is a conservative compatibility bound,
// not promotion of the host's Knowledge Pack capabilities or evidence levels.
func validateCompiledHost(gen domain.Generation, host string, detection hostcontext.HostDetection) error {
	entry, ok := gen.Spec.Hosts[host]
	if !ok || entry.HostVersion == "" || entry.Surface == "" {
		return fmt.Errorf("generation %s has no compiled host target for %s; recompile and activate the desired runtime before rollback", gen.Metadata.ID, host)
	}
	if detection.Host != host || detection.Version == "" || detection.Error != "" {
		return fmt.Errorf("cannot validate generation %s against unknown or mismatched host detection", gen.Metadata.ID)
	}
	if entry.HostVersion != detection.Version || entry.Surface != surfaceOf(detection) {
		return fmt.Errorf("generation %s was compiled for %s/%s %s, detected %s/%s %s; recompile and activate the desired runtime", gen.Metadata.ID, host, entry.Surface, entry.HostVersion, detection.Host, surfaceOf(detection), detection.Version)
	}
	return nil
}
