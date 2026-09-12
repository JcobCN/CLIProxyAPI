package auth

import (
	"context"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/activetracker"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// trackActiveRequestModel records the client-requested model on the active
// request tracker entry created by the API auth middleware. It is a noop when
// the request is not tracked (e.g. management traffic or missing gin context).
func trackActiveRequestModel(ctx context.Context, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) {
	if ctx == nil {
		return
	}
	ginCtx, ok := ctx.Value("gin").(interface{ Get(string) (any, bool) })
	if !ok || ginCtx == nil {
		return
	}
	raw, exists := ginCtx.Get(activetracker.GinContextKey)
	if !exists {
		return
	}
	entry, ok := raw.(*activetracker.ActiveRequest)
	if !ok {
		return
	}
	model := ""
	if rawModel, ok := opts.Metadata[cliproxyexecutor.RequestedModelMetadataKey]; ok {
		model, _ = rawModel.(string)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(req.Model)
	}
	activetracker.Default.SetModel(entry, model)
}
