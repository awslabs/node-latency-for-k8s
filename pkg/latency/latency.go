/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package latency provides a convenient abstraction around timing the startup and bootstrap of a Kubernetes node.
// latency provides an extensibility mechanism to register custom sources and events, but also ships with a set of default sources and events.
package latency

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/olekukonko/tablewriter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
	"go.uber.org/multierr"

	"github.com/awslabs/node-latency-for-k8s/pkg/sources"
	"github.com/awslabs/node-latency-for-k8s/pkg/sources/awsnode"
	imdssrc "github.com/awslabs/node-latency-for-k8s/pkg/sources/imds"
	"github.com/awslabs/node-latency-for-k8s/pkg/sources/messages"
)

// Measurer holds registered sources and events to use for timing runs
type Measurer struct {
	sources    map[string]sources.Source
	events     []*Event
	metadata   *Metadata
	imdsClient *imds.Client
}

// Measurement is a specific timing produced from a Measurer run
type Measurement struct {
	Metadata *Metadata `json:"metadata"`
	Timings  []*Timing `json:"timings"`
}

// Metadata provides data about the node where measurements are executed
type Metadata struct {
	Region           string `json:"region"`
	InstanceType     string `json:"instanceType"`
	InstanceID       string `json:"instanceID"`
	AccountID        string `json:"accountID"`
	Architecture     string `json:"architecture"`
	AvailabilityZone string `json:"availabilityZone"`
	PrivateIP        string `json:"privateIP"`
	AMIID            string `json:"amiID"`
}

// Timing is a specific instance of an Event timing
type Timing struct {
	Event     *Event        `json:"event"`
	Timestamp time.Time     `json:"timestamp"`
	T         time.Duration `json:"seconds"`
	Error     error         `json:"error"`
}

// Event defines what is being timed from a specific source
type Event struct {
	Name            string `json:"name"`
	Metric          string `json:"metric"`
	Search          string `json:"search"`
	FirstOccurrence bool   `json:"firstOccurrence"`
	Terminal        bool   `json:"terminal"`
	Src             string `json:"src"`
	src             sources.Source
}

var (
	vmInit                = regexp.MustCompile(`.*kernel: Linux version.*`)
	networkStart          = regexp.MustCompile(`.*Reached target Network \(Pre\).*`)
	networkReady          = regexp.MustCompile(`.*Reached target Network\..*`)
	cloudInitInitialStart = regexp.MustCompile(`.*cloud-init: Cloud-init v.* running 'init'.*`)
	cloudInitConfigStart  = regexp.MustCompile(`.*cloud-init: Cloud-init v.* running 'modules:config'.*`)
	cloudInitFinalStart   = regexp.MustCompile(`.*cloud-init: Cloud-init v.* running 'modules:final'.*`)
	cloudInitFinalFinish  = regexp.MustCompile(`.*cloud-init: Cloud-init v.* finished`)
	containerdStart       = regexp.MustCompile(`.*Starting containerd container runtime.*`)
	containerdInitialized = regexp.MustCompile(`.*Started containerd container runtime.*`)
	kubeletStart          = regexp.MustCompile(`.*Starting Kubernetes Kubelet.*`)
	kubeletInitialized    = regexp.MustCompile(`.*Started kubelet.*`)
	kubeletRegistered     = regexp.MustCompile(`.*Successfully registered node.*`)
	kubeProxyStart        = regexp.MustCompile(`.*CreateContainer within sandbox .*Name:kube-proxy.* returns container id.*`)
	vpcCNIInitStart       = regexp.MustCompile(`.*CreateContainer within sandbox .*Name:aws-vpc-cni-init.* returns container id.*`)
	awsNodeStart          = regexp.MustCompile(`.*CreateContainer within sandbox .*Name:aws-node.* returns container id.*`)
	vpcCNIInitialized     = regexp.MustCompile(`.*Successfully copied CNI plugin binary and config file.*`)
	nodeReady             = regexp.MustCompile(`.*event="NodeReady".*`)
	podReady              = regexp.MustCompile(`.*default/.* Type:ContainerStarted.*`)
)

// New creates a new instance of a Measurer
func New() *Measurer {
	return &Measurer{
		sources: make(map[string]sources.Source),
	}
}

// WithIMDS is a builder func that adds an EC2 Instance Metadata Service (IMDS) client to a Measurer
func (m *Measurer) WithIMDS(imdsClient *imds.Client) *Measurer {
	m.imdsClient = imdsClient
	return m
}

// MustWithDefaultConfig registers the default sources and events to the Measurer and panics if any errors occur
func (m *Measurer) MustWithDefaultConfig() *Measurer {
	return lo.Must(m.RegisterDefaultSources().RegisterDefaultEvents())
}

// RegisterSources registers n sources to the Measurer
func (m *Measurer) RegisterSources(srcs ...sources.Source) *Measurer {
	for _, src := range srcs {
		m.sources[src.Name()] = src
	}
	return m
}

// RegisterEvents registers n events to the Measurer. The sources for the events must already be registered.
func (m *Measurer) RegisterEvents(events ...*Event) (*Measurer, error) {
	var errs error
	for _, e := range events {
		src, ok := m.GetSource(e.Src)
		if !ok {
			errs = multierr.Append(errs, fmt.Errorf("unable to register event \"%s\" because source \"%s\" is not registered", e.Name, e.Src))
			continue
		}
		e.src = src
		m.events = append(m.events, e)
	}
	return m, errs
}

// GetSource looks up a registered source by name
func (m *Measurer) GetSource(name string) (sources.Source, bool) {
	src, ok := m.sources[name]
	return src, ok
}

// Measure executes a single timing run with the registered sources and events
func (m *Measurer) Measure(ctx context.Context) *Measurement {
	var timings []*Timing
	for _, event := range m.events {
		ts, err := event.src.Find(event.Search, event.FirstOccurrence)
		timings = append(timings, &Timing{
			Event:     event,
			Timestamp: ts,
			Error:     err,
		})
	}
	// Sort timings so they are in chronological order
	sort.Slice(timings, func(i, j int) bool {
		return timings[i].Timestamp.UnixMicro() < timings[j].Timestamp.UnixMicro()
	})
	// Add normalized time delta
	for _, t := range timings {
		t.T = t.Timestamp.Sub(timings[0].Timestamp)
	}
	// ignore metadata errors
	metadata, _ := m.getMetadata(ctx)
	return &Measurement{
		Metadata: metadata,
		Timings:  timings,
	}
}

// MeasureUntil executes timing runs with the registered sources and events until all terminal events have timings or the timeout is reached
func (m *Measurer) MeasureUntil(ctx context.Context, timeout time.Duration, retryDelay time.Duration) *Measurement {
	startTime := time.Now().UTC()
	anyErr := true
	var measurement *Measurement
	for anyErr && time.Since(startTime) < timeout {
		anyErr = false
		measurement = m.Measure(ctx)
		for _, m := range measurement.Timings {
			if m.Error != nil {
				anyErr = true
				log.Printf("Unable to retrieve timing for Event \"%s\": %v\n", m.Event.Name, m.Error)
			}
		}

		done := false
		// check if there are any terminal events, if so, check if they have completed successfully
		// if all events are not terminal, then try to time all events without errors until the timeout is reached.
		if !lo.EveryBy(measurement.Timings, func(t *Timing) bool { return !t.Event.Terminal }) {
			// Check if all terminal events completed timings successfully
			done = lo.EveryBy(measurement.Timings, func(t *Timing) bool {
				if !t.Event.Terminal {
					return true
				}
				return t.Error == nil
			})
		}

		if done {
			return measurement
		} else if anyErr {
			for _, s := range m.sources {
				s.ClearCache()
			}
			time.Sleep(retryDelay)
		} else {
			return measurement
		}
	}
	return measurement
}

// getMetadata populates the metadata for a Measurement
func (m *Measurer) getMetadata(ctx context.Context) (*Metadata, error) {
	if m.metadata != nil {
		return m.metadata, nil
	}
	if m.imdsClient == nil {
		return nil, errors.New("imds client is nil")
	}
	idDoc, err := m.imdsClient.GetInstanceIdentityDocument(ctx, &imds.GetInstanceIdentityDocumentInput{})
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve instance-identity document: %w", err)
	}
	return &Metadata{
		Region:           idDoc.Region,
		InstanceType:     idDoc.InstanceType,
		InstanceID:       idDoc.InstanceID,
		AccountID:        idDoc.AccountID,
		Architecture:     idDoc.Architecture,
		AvailabilityZone: idDoc.AvailabilityZone,
		AMIID:            idDoc.ImageID,
		PrivateIP:        idDoc.PrivateIP,
	}, nil
}

// Chart generates a markdown chart view of a Measurement
func (m *Measurement) Chart() {
	if m.Metadata != nil {
		fmt.Printf("### %s (%s) | %s | %s | %s | %s\n",
			m.Metadata.InstanceID, m.Metadata.PrivateIP, m.Metadata.InstanceType, m.Metadata.Architecture,
			m.Metadata.AvailabilityZone, m.Metadata.AMIID)
	}
	var data [][]string
	for _, t := range m.Timings {
		data = append(data, []string{
			t.Event.Name, t.Timestamp.Format("2006-01-02T15:04:05Z"), fmt.Sprintf("%.0fs", t.T.Seconds()),
		})
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Event", "Timestamp", "T"})
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.AppendBulk(data)
	table.Render()
}

// RegisterMetrics registers prometheus metrics based on a measurement
func (m *Measurement) RegisterMetrics(register prometheus.Registerer, experimentDimension string) {
	dimensions := m.metricDimensions(experimentDimension)
	labels := lo.Keys(dimensions)

	for _, timing := range m.Timings {
		collector := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: timing.Event.Metric,
		}, labels)
		if err := register.Register(collector); err != nil {
			log.Printf("error registering metric %s: %v", timing.Event.Metric, err)
		}
		collector.With(dimensions).Set(timing.T.Seconds())
	}
}

// EmitCloudWatchMetrics posts metric data to CloudWatch based on a Measurement
func (m *Measurement) EmitCloudWatchMetrics(ctx context.Context, cw *cloudwatch.Client, experimentDimension string) error {
	var errs error
	dimensions := m.metricDimensions(experimentDimension)
	for _, timing := range m.Timings {
		if _, err := cw.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
			Namespace: aws.String("KubernetesNodeLatency"),
			MetricData: []types.MetricDatum{
				{
					MetricName: aws.String(timing.Event.Metric),
					Value:      aws.Float64(timing.T.Seconds()),
					Unit:       types.StandardUnitSeconds,
					Dimensions: lo.MapToSlice(dimensions, func(k, v string) types.Dimension {
						return types.Dimension{
							Name:  aws.String(k),
							Value: aws.String(v),
						}
					}),
				},
			},
		}); err != nil {
			errs = multierr.Append(errs, err)
		}
	}
	return errs
}

// metricDimensions is a helper to construct default metric dimensions for both cloudwatch and prometheus
func (m *Measurement) metricDimensions(experimentDimension string) map[string]string {
	dimensions := map[string]string{
		"experiment": experimentDimension,
	}
	if m.Metadata != nil {
		dimensions = lo.Assign(dimensions, map[string]string{
			"instanceType":     m.Metadata.InstanceType,
			"amiID":            m.Metadata.AMIID,
			"region":           m.Metadata.Region,
			"availabilityZone": m.Metadata.AvailabilityZone,
		})
	}
	return dimensions
}

// RegisterDefaultSources registers the default sources to the Measurer
func (m *Measurer) RegisterDefaultSources() *Measurer {
	m.RegisterSources([]sources.Source{
		messages.New(messages.DefaultPath),
		awsnode.New(awsnode.DefaultPath),
	}...)
	if m.imdsClient != nil {
		m.RegisterSources(imdssrc.New(m.imdsClient))
	}
	return m
}

// RegisterDefaultEvents registers all default events shipped
func (m *Measurer) RegisterDefaultEvents() (*Measurer, error) {
	return m.RegisterEvents([]*Event{
		{
			Name:   "Instance Requested",
			Metric: "instance_requested",
			Src:    imdssrc.Name,
			Search: imdssrc.RequestedTime,
		},
		{
			Name:   "Instance Pending",
			Metric: "instance_pending",
			Src:    imdssrc.Name,
			Search: imdssrc.PendingTime,
		},
		{
			Name:   "VM Initialized",
			Metric: "vm_initialized",
			Src:    messages.Name,
			Search: vmInit.String(),
		},
		{
			Name:   "Network Start",
			Metric: "network_start",
			Src:    messages.Name,
			Search: networkStart.String(),
		},
		{
			Name:   "Network Ready",
			Metric: "network_ready",
			Src:    messages.Name,
			Search: networkReady.String(),
		},
		{
			Name:   "Cloud-Init Initial Start",
			Metric: "cloudinit_initial_start",
			Src:    messages.Name,
			Search: cloudInitInitialStart.String(),
		},
		{
			Name:   "Cloud-Init Config Start",
			Metric: "cloudinit_config_start",
			Src:    messages.Name,
			Search: cloudInitConfigStart.String(),
		},
		{
			Name:   "Cloud-Init Final Start",
			Metric: "cloudinit_final_start",
			Src:    messages.Name,
			Search: cloudInitFinalStart.String(),
		},
		{
			Name:   "Cloud-Init Final Finish",
			Metric: "cloudinit_final_finish",
			Src:    messages.Name,
			Search: cloudInitFinalFinish.String(),
		},
		{
			Name:   "Containerd Start",
			Metric: "conatinerd_start",
			Src:    messages.Name,
			Search: containerdStart.String(),
		},
		{
			Name:   "Containerd Initialized",
			Metric: "conatinerd_initialized",
			Src:    messages.Name,
			Search: containerdInitialized.String(),
		},
		{
			Name:   "Kubelet Start",
			Metric: "kubelet_start",
			Src:    messages.Name,
			Search: kubeletStart.String(),
		},
		{
			Name:   "Kubelet Initialized",
			Metric: "kubelet_initialized",
			Src:    messages.Name,
			Search: kubeletInitialized.String(),
		},
		{
			Name:   "Kubelet Registered",
			Metric: "kubelet_registered",
			Src:    messages.Name,
			Search: kubeletRegistered.String(),
		},
		{
			Name:   "Kube-Proxy Start",
			Metric: "kube_proxy_start",
			Src:    messages.Name,
			Search: kubeProxyStart.String(),
		},
		{
			Name:   "VPC CNI Init Start",
			Metric: "vpc_cni_init_start",
			Src:    messages.Name,
			Search: vpcCNIInitStart.String(),
		},
		{
			Name:   "AWS Node Start",
			Metric: "aws_node_start",
			Src:    messages.Name,
			Search: awsNodeStart.String(),
		},
		{
			Name:   "VPC CNI Plugin Initialized",
			Metric: "vpc_cni_plugin_initialized",
			Src:    awsnode.Name,
			Search: vpcCNIInitialized.String(),
		},
		{
			Name:     "Node Ready",
			Metric:   "node_ready",
			Src:      messages.Name,
			Terminal: true,
			Search:   nodeReady.String(),
		},
		{
			Name:            "Pod Ready",
			Metric:          "pod_ready",
			Src:             messages.Name,
			Search:          podReady.String(),
			Terminal:        true,
			FirstOccurrence: true,
		},
	}...)
}
