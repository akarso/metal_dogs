package metaldogs

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/observability"
)

const obsPingJob = "obs.ping"

func registerObsPing(app *plugin.App) error {
	sdk := pluginsdk.New(app, PluginName)
	return sdk.Integration(PluginName).RegisterManual(obsPingJob, obsPingHandler(app))
}

func obsPingHandler(app *plugin.App) extapi.SyncJobHandler {
	return func(_ context.Context, job extapi.SyncJobContext) error {
		fields := observability.JobLogFields(job)
		switch {
		case job.Logger != nil:
			job.Logger.Info("metaldogs.obs.ping", fields)
		case app != nil && app.Logger != nil:
			app.Logger.Info("metaldogs.obs.ping", fields)
		}
		return nil
	}
}
