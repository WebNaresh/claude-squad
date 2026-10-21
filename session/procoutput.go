package session

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// procTimeout caps a ps or lsof call. Under heavy memory pressure ps once
// took minutes per call; cs kept starting new ones every few seconds, and 35
// were running at once (load average 225).
const procTimeout = 5 * time.Second

// ProcOutput runs a process-listing command (ps, lsof) and returns its
// output, killing it after procTimeout.
func ProcOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), procTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if ctx.Err() != nil {
		// Killed for taking too long: say so, not "exit status" (lsof's
		// exit 1 means "nothing found", a timeout means "don't know").
		return out, fmt.Errorf("%s took over %s: %w", name, procTimeout, ctx.Err())
	}
	return out, err
}
