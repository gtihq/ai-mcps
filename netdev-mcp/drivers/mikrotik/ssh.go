package mikrotik

import (
	"context"
	"time"

	"github.com/gtihq/ai-mcps/netdev-mcp/device"
	"github.com/gtihq/ai-mcps/netdev-mcp/drivers/sshutil"
)

// sshExec runs one command over SSH. RouterOS has no REST endpoint for ping
// or config export, so those tools use the same fallback as routeros-mcp.
func sshExec(ctx context.Context, cfg device.Config, timeout time.Duration, command string) (string, error) {
	return sshutil.Exec(ctx, cfg, timeout, command)
}
