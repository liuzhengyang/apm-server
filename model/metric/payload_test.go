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

package metric

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/apm-server/config"
	m "github.com/elastic/apm-server/model"
	"github.com/elastic/beats/libbeat/common"
)

// assertMetricsMatch is an equality test for a metric as sample order is not important
func assertMetricsMatch(t *testing.T, expected, actual metric) bool {
	samplesMatch := assert.ElementsMatch(t, expected.samples, actual.samples)
	expected.samples = nil
	actual.samples = nil
	nonSamplesMatch := assert.Equal(t, expected, actual)

	return assert.True(t, samplesMatch && nonSamplesMatch,
		fmt.Sprintf("metrics mismatch\nexpected:%#v\n   actual:%#v", expected, actual))
}

func TestPayloadDecode(t *testing.T) {
	timestamp := "2017-05-30T18:53:27.154Z"
	timestampParsed, _ := time.Parse(time.RFC3339, timestamp)
	pid, ip := 1, "127.0.0.1"
	for _, test := range []struct {
		input map[string]interface{}
		err   error
		p     *Payload
	}{
		{input: nil, err: nil, p: nil},
		{
			input: map[string]interface{}{"service": 123},
			err:   errors.New("Invalid type for service"),
		},
		{
			input: map[string]interface{}{"system": 123},
			err:   errors.New("Invalid type for system"),
		},
		{
			input: map[string]interface{}{"process": 123},
			err:   errors.New("Invalid type for process"),
		},
		{
			input: map[string]interface{}{},
			err:   nil,
			p: &Payload{
				Service: m.Service{}, System: nil,
				Process: nil, Metrics: []*metric{},
			},
		},
		{
			input: map[string]interface{}{
				"system": map[string]interface{}{"ip": ip},
				"service": map[string]interface{}{
					"name": "a",
					"agent": map[string]interface{}{
						"name": "ag", "version": "1.0",
					}},
				"process": map[string]interface{}{"pid": 1.0},
				"metrics": []interface{}{
					map[string]interface{}{
						"timestamp": timestamp,
						"samples":   map[string]interface{}{},
					},
				},
			},
			err: nil,
			p: &Payload{
				Service: m.Service{
					Name: "a", Agent: m.Agent{Name: "ag", Version: "1.0"}},
				System:  &m.System{IP: &ip},
				Process: &m.Process{Pid: pid},
				Metrics: []*metric{
					{
						samples:   []*sample{},
						tags:      nil,
						timestamp: timestampParsed,
					},
				},
			},
		},
		{
			input: map[string]interface{}{
				"system": map[string]interface{}{"ip": ip},
				"service": map[string]interface{}{
					"name": "a",
					"agent": map[string]interface{}{
						"name": "ag", "version": "1.0",
					}},
				"process": map[string]interface{}{"pid": 1.0},
				"metrics": []interface{}{
					map[string]interface{}{
						"timestamp": timestamp,
						"samples": map[string]interface{}{
							"invalid.metric": map[string]interface{}{
								"value": "foo",
							},
						},
					},
				},
			},
			err: errors.New("Error fetching field"),
		},
		{
			input: map[string]interface{}{
				"system": map[string]interface{}{"ip": ip},
				"service": map[string]interface{}{
					"name": "a",
					"agent": map[string]interface{}{
						"name": "ag", "version": "1.0",
					}},
				"process": map[string]interface{}{"pid": 1.0},
				"metrics": []interface{}{
					map[string]interface{}{
						"tags": map[string]interface{}{
							"a.tag": "a.tag.value",
						},
						"timestamp": timestamp,
						"samples": map[string]interface{}{
							"a.counter": map[string]interface{}{
								"value": json.Number("612"),
							},
							"some.gauge": map[string]interface{}{
								"value": json.Number("9.16"),
							},
						},
					},
				},
			},
			err: nil,
			p: &Payload{
				Service: m.Service{Name: "a", Agent: m.Agent{Name: "ag", Version: "1.0"}},
				System:  &m.System{IP: &ip},
				Process: &m.Process{Pid: pid},
				Metrics: []*metric{
					{
						samples: []*sample{
							{
								name:  "some.gauge",
								value: 9.16,
							},
							{
								name:  "a.counter",
								value: 612,
							},
						},
						tags: common.MapStr{
							"a.tag": "a.tag.value",
						},
						timestamp: timestampParsed,
					},
				},
			},
		},
	} {
		payloadInterface, err := DecodePayload(test.input)

		payload, ok := payloadInterface.(*Payload)
		if payloadInterface != nil {
			require.True(t, ok)
		}

		// compare metrics separately as they may be ordered differently
		if test.p != nil && test.p.Metrics != nil {
			for i := range test.p.Metrics {
				if test.p.Metrics[i] == nil {
					continue
				}
				want := *test.p.Metrics[i]
				got := *payload.Metrics[i]
				assertMetricsMatch(t, want, got)
			}
			// leave everything else in payload for comparison
			test.p.Metrics = nil
			payload.Metrics = nil
		}
		// compare remaining payload
		assert.Equal(t, test.p, payload)
		if test.err != nil {
			assert.Error(t, err)
		}
	}
}

func TestPayloadTransform(t *testing.T) {
	svc := m.Service{Name: "myservice"}
	timestamp := time.Now()

	tests := []struct {
		Payload Payload
		Output  []common.MapStr
		Msg     string
	}{
		{
			Payload: Payload{Service: svc, Metrics: []*metric{}},
			Output:  nil,
			Msg:     "Empty metric Array",
		},
		{
			Payload: Payload{Service: svc, Metrics: []*metric{{timestamp: timestamp}}},
			Output: []common.MapStr{
				{
					"context": common.MapStr{
						"service": common.MapStr{
							"agent": common.MapStr{"name": "", "version": ""},
							"name":  "myservice",
						},
					},
					"processor": common.MapStr{"event": "metric", "name": "metric"},
				},
			},
			Msg: "Payload with empty metric.",
		},
		{
			Payload: Payload{
				Service: svc,
				Metrics: []*metric{
					{
						tags:      common.MapStr{"a.tag": "a.tag.value"},
						timestamp: timestamp,
						samples: []*sample{
							{
								name:  "a.counter",
								value: 612,
							},
							{
								name:  "some.gauge",
								value: 9.16,
							},
						},
					},
				},
			},
			Output: []common.MapStr{
				{
					"context": common.MapStr{
						"service": common.MapStr{
							"name":  "myservice",
							"agent": common.MapStr{"name": "", "version": ""},
						},
						"tags": common.MapStr{
							"a.tag": "a.tag.value",
						},
					},
					"a":         common.MapStr{"counter": float64(612)},
					"some":      common.MapStr{"gauge": float64(9.16)},
					"processor": common.MapStr{"event": "metric", "name": "metric"},
				},
			},
			Msg: "Payload with valid metric.",
		},
	}

	for idx, test := range tests {
		conf := config.Config{}
		outputEvents := test.Payload.Transform(conf)
		for j, outputEvent := range outputEvents {
			assert.Equal(t, test.Output[j], outputEvent.Fields, fmt.Sprintf("Failed at idx %v; %s", idx, test.Msg))
			assert.Equal(t, timestamp, outputEvent.Timestamp, fmt.Sprintf("Bad timestamp at idx %v; %s", idx, test.Msg))
		}
	}
}
