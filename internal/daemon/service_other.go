//go:build !windows

package daemon

import "context"

func RunService(ctx context.Context, options Options) error { return Run(ctx, options) }
