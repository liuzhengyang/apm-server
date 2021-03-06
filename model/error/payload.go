// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package error

import (
	"github.com/elastic/apm-server/config"
	m "github.com/elastic/apm-server/model"
	"github.com/elastic/apm-server/utility"
	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/monitoring"
)

var (
	transformations   = monitoring.NewInt(Metrics, "transformations")
	errorCounter      = monitoring.NewInt(Metrics, "errors")
	stacktraceCounter = monitoring.NewInt(Metrics, "stacktraces")
	frameCounter      = monitoring.NewInt(Metrics, "frames")
	processorEntry    = common.MapStr{"name": processorName, "event": errorDocType}
)

type Payload struct {
	Service m.Service
	System  *m.System
	Process *m.Process
	User    *m.User
	Events  []*Event
}

func DecodePayload(raw map[string]interface{}) (m.Payload, error) {
	if raw == nil {
		return nil, nil
	}
	pa := Payload{}

	var err error
	service, err := m.DecodeService(raw["service"], err)
	if service != nil {
		pa.Service = *service
	}
	pa.System, err = m.DecodeSystem(raw["system"], err)
	pa.Process, err = m.DecodeProcess(raw["process"], err)
	pa.User, err = m.DecodeUser(raw["user"], err)
	if err != nil {
		return nil, err
	}

	decoder := utility.ManualDecoder{}
	errs := decoder.InterfaceArr(raw, "errors")
	pa.Events = make([]*Event, len(errs))
	err = decoder.Err
	for idx, errData := range errs {
		pa.Events[idx], err = DecodeEvent(errData, err)
	}
	return &pa, err
}

func (pa *Payload) Transform(conf config.Config) []beat.Event {
	transformations.Inc()
	errorCounter.Add(int64(len(pa.Events)))
	logp.NewLogger("transform").Debugf("Transform error events: events=%d, service=%s, agent=%s:%s", len(pa.Events), pa.Service.Name, pa.Service.Agent.Name, pa.Service.Agent.Version)

	context := m.NewContext(&pa.Service, pa.Process, pa.System, pa.User)

	var events []beat.Event
	for idx := 0; idx < len(pa.Events); idx++ {
		event := pa.Events[idx]
		if event.Exception != nil {
			addStacktraceCounter(event.Exception.Stacktrace)
		}
		if event.Log != nil {
			addStacktraceCounter(event.Log.Stacktrace)
		}
		context := context.Transform(event.Context)
		ev := beat.Event{
			Fields: common.MapStr{
				"processor":  processorEntry,
				errorDocType: event.Transform(conf, pa.Service),
				"context":    context,
			},
			Timestamp: event.Timestamp,
		}
		if event.Transaction != nil {
			ev.Fields["transaction"] = common.MapStr{"id": event.Transaction.Id}
		}
		events = append(events, ev)
		pa.Events[idx] = nil
	}
	return events
}

func addStacktraceCounter(st m.Stacktrace) {
	if frames := len(st); frames > 0 {
		stacktraceCounter.Inc()
		frameCounter.Add(int64(frames))
	}
}
