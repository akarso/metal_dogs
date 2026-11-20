package metaldogs

import (
	"context"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/observability"
)

const obsPingJob = "obs.ping"

type syncRegistrar interface {
	RegisterManual(name string, handler extapi.SyncJobHandler, opts ...pluginsdk.SyncJobOption) error
}

func registerObsPing(reg syncRegistrar, log pluginsdk.Logger) error {
	return reg.RegisterManual(obsPingJob, obsPingHandler(log))
}

func obsPingHandler(log pluginsdk.Logger) extapi.SyncJobHandler {
	return func(_ context.Context, job extapi.SyncJobContext) error {
		fields := observability.JobLogFields(job)
		switch {
		case job.Logger != nil:
			job.Logger.Info("metaldogs.obs.ping", fields)
		case log != nil:
			log.Info("metaldogs.obs.ping", fields)
		}
		return nil
	}
}
