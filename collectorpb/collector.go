package collectorpb

import (
    "github.com/lightstep/lightstep-tracer-protos/go/lightstep/collector"
)

type ReportResponseWrapper struct {
    *collectorpb.ReportResponse
}

func (wrapper *ReportResponseWrapper) Disable() bool {
	for _, command := range wrapper.GetCommands() {
		if command.Disable {
			return true
		}
	}
	return false
}
