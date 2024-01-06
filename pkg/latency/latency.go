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
	"io"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelMetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/olekukonko/tablewriter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/lo"
	"go.uber.org/multierr"
	"k8s.io/client-go/kubernetes"

	"github.com/awslabs/node-latency-for-k8s/pkg/sources"
	"github.com/awslabs/node-latency-for-k8s/pkg/sources/awsnode"
	ec2src "github.com/awslabs/node-latency-for-k8s/pkg/sources/ec2"
	imdssrc "github.com/awslabs/node-latency-for-k8s/pkg/sources/imds"
	k8ssrc "github.com/awslabs/node-latency-for-k8s/pkg/sources/k8s"
	"github.com/awslabs/node-latency-for-k8s/pkg/sources/messages"
)

// Measurer holds registered sources and events to use for timing runs
type Measurer struct {
	sources      map[string]sources.Source
	events       []*sources.Event
	metadata     *Metadata
	imdsClient   *imds.Client
	ec2Client    *ec2.Client
	k8sClientset *kubernetes.Clientset
	podNamespace string
	nodeName     string
}

// Measurement is a specific timing produced from a Measurer run
type Measurement struct {
	Metadata *Metadata         `json:"metadata"`
	Timings  []*sources.Timing `json:"timings"`
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

// ChartOptions allows configuration of the markdown chart
type ChartOptions struct {
	HiddenColumns []string
}

type OTeL struct {
	Reader        *metric.ManualReader
	MeterProvider *metric.MeterProvider
	Exporter      metric.Exporter
	Context       context.Context
	OneShot       bool
	OTeLEndpoint  string
}

// Chart column label consts
const (
	ChartColumnEvent     = "Event"
	ChartColumnTimestamp = "Timestamp"
	ChartColumnT         = "T"
	ChartColumnComment   = "Comment"
	ServiceName          = "node-latency-for-k8s"
)

// Default Event regular expressions
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
	throttled             = regexp.MustCompile(`.*Waited for .* due to client-side throttling, not priority and fairness, request: .*`)
	podReadyStr           = `.*%s/.* Type:ContainerStarted.*`
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

// WithEC2Client is a builder func that adds an ec2 client to a Measurer
func (m *Measurer) WithEC2Client(ec2Client *ec2.Client) *Measurer {
	m.ec2Client = ec2Client
	return m
}

// WithK8sClientset is a builder func that adds a k8s clientset to a Measurer
func (m *Measurer) WithK8sClientset(clientset *kubernetes.Clientset) *Measurer {
	m.k8sClientset = clientset
	return m
}

// WithPodNamespace sets the pod namespace that will be queried to measure pod creation to running time
func (m *Measurer) WithPodNamespace(podNamespace string) *Measurer {
	m.podNamespace = podNamespace
	return m
}

// WithNodeName sets the node name that will be used to query for the first scheduled pod on the given node name
func (m *Measurer) WithNodeName(nodeName string) *Measurer {
	m.nodeName = nodeName
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
func (m *Measurer) RegisterEvents(events ...*sources.Event) (*Measurer, error) {
	var errs error
	for _, e := range events {
		src, ok := m.GetSource(e.SrcName)
		if !ok {
			errs = multierr.Append(errs, fmt.Errorf("unable to register event \"%s\" because source \"%s\" is not registered", e.Name, e.Src))
			continue
		}
		e.Src = src
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
	var timings []*sources.Timing
	for _, event := range m.events {
		results, err := event.Src.Find(event)
		if len(results) == 0 {
			results = []sources.FindResult{}
		}
		for _, result := range results {
			timings = append(timings, &sources.Timing{
				Event:     event,
				Timestamp: result.Timestamp,
				Comment:   result.Comment,
				Error:     multierr.Append(err, result.Err),
			})
		}
	}
	// Sort timings so they are in chronological order
	sort.Slice(timings, func(i, j int) bool {
		return timings[i].Timestamp.UnixMicro() < timings[j].Timestamp.UnixMicro()
	})

	// Find the last terminal event index to filter out everything past
	if _, lastTerminalIndex, ok := lo.FindLastIndexOf(timings, func(t *sources.Timing) bool {
		return t.Event.Terminal
	}); ok {
		timings = timings[:lastTerminalIndex+1]
	}
	firstSuccessfulTiming := timings[0]
	// Find first successful timing
	for _, t := range timings {
		if t.Error == nil {
			firstSuccessfulTiming = t
			break
		}
	}
	// Add normalized time delta
	for _, t := range timings {
		t.T = t.Timestamp.Sub(firstSuccessfulTiming.Timestamp)
	}
	// ignore metadata errors
	metadata, _ := m.getMetadata(ctx)
	return &Measurement{
		Metadata: metadata,
		Timings:  timings,
	}
}

// MeasureUntil executes timing runs with the registered sources and events until all terminal events have timings or the timeout is reached
func (m *Measurer) MeasureUntil(ctx context.Context, timeout time.Duration, retryDelay time.Duration) (*Measurement, error) {
	startTime := time.Now().UTC()
	var measurement *Measurement
	terminalEvents := lo.CountBy(m.events, func(e *sources.Event) bool { return e.Terminal })
	done := false
	for !done && time.Since(startTime) < timeout {
		done = false
		measurement = m.Measure(ctx)
		for _, m := range measurement.Timings {
			if m.Error != nil {
				log.Printf("Unable to retrieve timing for Event \"%s\": %v\n", m.Event.Name, m.Error)
			}
		}
		measuredEvents := lo.CountBy(measurement.Timings, func(t *sources.Timing) bool { return t.Error == nil })
		measuredTerminalEvents := lo.CountBy(measurement.Timings, func(t *sources.Timing) bool { return t.Event.Terminal && t.Error == nil })
		// check if there are any terminal events, if so, check if they have completed successfully
		if terminalEvents > 0 && terminalEvents == measuredTerminalEvents {
			done = true
			// if all events are not terminal, then try to time all events without errors until the timeout is reached.
		} else if terminalEvents == 0 && measuredEvents >= len(m.events) {
			done = true
		}

		if done {
			return measurement, nil
		}
		for _, s := range m.sources {
			s.ClearCache()
		}
		time.Sleep(retryDelay)
	}
	if terminalEvents > 0 {
		unmeasuredTerminalEvents := lo.Filter(m.events, func(e *sources.Event, _ int) bool {
			return e.Terminal && lo.CountBy(measurement.Timings, func(t *sources.Timing) bool { return t.Event.Name == e.Name }) == 0
		})
		unmeasuredTerminalEventNames := lo.Map(unmeasuredTerminalEvents, func(e *sources.Event, _ int) string { return e.Name })
		return measurement, fmt.Errorf("unable to measure terminal events: %v", unmeasuredTerminalEventNames)
	}
	unmeasuredEvents := lo.Filter(m.events, func(e *sources.Event, _ int) bool {
		return lo.CountBy(measurement.Timings, func(t *sources.Timing) bool { return t.Event.Name == e.Name }) == 0
	})
	unmeasuredEventNames := lo.Map(unmeasuredEvents, func(e *sources.Event, _ int) string { return e.Name })
	return measurement, fmt.Errorf("unable to measure events %v within timeout window", unmeasuredEventNames)
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
func (m *Measurement) Chart(opts ChartOptions) {
	if m.Metadata != nil {
		fmt.Printf("### %s (%s) | %s | %s | %s | %s\n",
			m.Metadata.InstanceID, m.Metadata.PrivateIP, m.Metadata.InstanceType, m.Metadata.Architecture,
			m.Metadata.AvailabilityZone, m.Metadata.AMIID)
	}
	table := tablewriter.NewWriter(os.Stdout)
	headers := []string{ChartColumnEvent, ChartColumnTimestamp, ChartColumnT, ChartColumnComment}
	table.SetHeader(filterColumns(opts.HiddenColumns, headers, headers))

	var data [][]string
	for _, t := range m.Timings {
		if t.Error != nil {
			log.Printf("Error with event \"%s\" timing: %v\n", t.Event.Name, t.Error)
			continue
		}
		data = append(data, filterColumns(opts.HiddenColumns, headers, []string{
			t.Event.Name,
			t.Timestamp.Format("2006-01-02T15:04:05Z"),
			fmt.Sprintf("%.0fs", t.T.Seconds()),
			t.Comment,
		}))
	}

	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.AppendBulk(data)
	table.Render()
}

// filterColumns will filter out specified columns via case insensitive string matching
// This is used for generating the markdown chart
func filterColumns(hiddenColumns []string, headers []string, data []string) []string {
	// Find hidden columns indexes
	var hiddenColIndexes []int
	for i, header := range headers {
		for _, hiddenCol := range hiddenColumns {
			if strings.EqualFold(hiddenCol, header) {
				hiddenColIndexes = append(hiddenColIndexes, i)
			}
		}
	}
	// Filter data to exclude any hidden columns
	var filteredData []string
	for i, col := range data {
		if !lo.Contains(hiddenColIndexes, i) {
			filteredData = append(filteredData, col)
		}
	}
	return filteredData
}

// RegisterMetrics registers prometheus metrics based on a measurement
func (m *Measurement) RegisterMetrics(register prometheus.Registerer, experimentDimension string) {
	dimensions := m.metricDimensions(experimentDimension)
	labels := lo.Keys(dimensions)

	metricCollectors := map[string]*prometheus.GaugeVec{}
	for _, timing := range lo.UniqBy(m.Timings, func(t *sources.Timing) string { return t.Event.Metric }) {
		collector := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: timing.Event.Metric,
		}, labels)
		if err := register.Register(collector); err != nil {
			log.Printf("error registering metric %s: %v", timing.Event.Metric, err)
		}
		metricCollectors[timing.Event.Metric] = collector
	}
	for _, timing := range m.Timings {
		collector, ok := metricCollectors[timing.Event.Metric]
		if !ok {
			log.Printf("error emitting metric for %s", timing.Event.Metric)
			continue
		}
		collector.With(dimensions).Set(timing.T.Seconds())
	}
}

func newResource(version string) (*resource.Resource, error) {
	return resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName(ServiceName),
			semconv.ServiceVersion(version),
		))
}

func newMeterProvider(res *resource.Resource, reader *metric.ManualReader) *metric.MeterProvider {
	return metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(reader),
	)

}

func (o *OTeL) SendMetrics() error {
	defer func() {
		if err := o.MeterProvider.Shutdown(o.Context); err != nil {
			log.Println(err)
		}
	}()

	collectedMetrics := &metricdata.ResourceMetrics{}
	if err := o.Reader.Collect(o.Context, collectedMetrics); err != nil {
		return err
	}

	if err := o.Exporter.Export(o.Context, collectedMetrics); err != nil {
		return err
	}

	log.Printf("Emitting OTeL metrics to backend - %s", o.OTeLEndpoint)

	return nil
}

func (m *Measurement) createOTeLAttributes(experimentDimension string) []attribute.KeyValue {
	attributes := []attribute.KeyValue{}
	for k, v := range m.metricDimensions(experimentDimension) {
		attributes = append(attributes, attribute.String(k, v))
	}

	return attributes
}

// RegisterOTeLMetrics registers OTeL metrics based on a measurement that will be emitted once or continuously
func (m *Measurement) RegisterOTeLMetrics(ctx context.Context, experimentDimension, version, endpoint string) (*OTeL, error) {
	oTel := &OTeL{}

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return oTel, err
	}

	reader := metric.NewManualReader()
	if reader == nil {
		return oTel, errors.New("failed to instantiate OTeL metrics Reader")
	}

	res, err := newResource(version)
	if err != nil {
		return oTel, err
	}

	meterProvider := newMeterProvider(res, reader)

	otel.SetMeterProvider(meterProvider)
	mtr := otel.Meter(ServiceName)

	commonAttributes := m.createOTeLAttributes(experimentDimension)

	for _, timing := range lo.UniqBy(m.Timings, func(t *sources.Timing) string { return t.Event.Metric }) {
		elapsedTime := timing.T.Seconds()
		metricTimestamp := timing.Timestamp.String()

		attributesWithTimestamp := append([]attribute.KeyValue{}, commonAttributes...)
		attributesWithTimestamp = append(attributesWithTimestamp, attribute.String("metric_timestamp", metricTimestamp))

		if _, err = mtr.Int64ObservableGauge(timing.Event.Metric,
			otelMetric.WithUnit("{seconds}"),
			otelMetric.WithInt64Callback(func(_ context.Context, o otelMetric.Int64Observer) error {
				o.Observe(int64(elapsedTime), otelMetric.WithAttributes(attributesWithTimestamp...))
				return nil
			}),
		); err != nil {
			return oTel, err
		}
	}

	return &OTeL{
		Context:       ctx,
		Reader:        reader,
		MeterProvider: meterProvider,
		Exporter:      metricExporter,
		OTeLEndpoint:  endpoint,
	}, nil

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
	var year int

	if m.ec2Client != nil {
		instanceID := ""
		if m.imdsClient != nil {
			md, err := m.getMetadata(context.TODO())
			if err != nil {
				log.Printf("unable to retrieve instance-id to register the ec2 event source: %s", err)
			} else {
				instanceID = md.InstanceID
			}
		}

		// describe instance in order to get the year instance was launched
		output, err := m.ec2Client.DescribeInstances(context.TODO(),
			&ec2.DescribeInstancesInput{
				InstanceIds: []string{instanceID},
			})
		if err != nil || (len(output.Reservations) == 0 || len(output.Reservations[0].Instances) == 0) {
			fmt.Println("Error describing instance:", err)
		}

		// Access the LaunchTime of the first instance in the reservation
		launchTime := output.Reservations[0].Instances[0].LaunchTime

		// Print the year of the launch time
		year = launchTime.Year()
		m.RegisterSources(ec2src.New(m.ec2Client, instanceID, m.nodeName))
	}
	if m.imdsClient != nil {
		m.RegisterSources(imdssrc.New(m.imdsClient))
	}
	if m.k8sClientset != nil && m.podNamespace != "" {
		if m.nodeName == "" && m.imdsClient != nil {
			out, err := m.imdsClient.GetMetadata(context.TODO(), &imds.GetMetadataInput{Path: "/hostname"})
			if err != nil {
				log.Printf("unable to register K8s source because node name is required and is unable to be retrieved via EC2 IMDS: %v\n", err)
			}
			dnsName, err := io.ReadAll(out.Content)
			if err != nil {
				log.Printf("unable to register K8s source because node name is required and is unable to be read via EC2 IMDS: %v\n", err)
			}
			m.nodeName = string(dnsName)
		}
		if m.nodeName != "" {
			m.RegisterSources(k8ssrc.New(m.k8sClientset, m.nodeName, m.podNamespace))
		}
	}
	m.RegisterSources([]sources.Source{
		messages.New(messages.DefaultPath, year),
		awsnode.New(awsnode.DefaultPath, year),
	}...)
	return m
}

// RegisterDefaultEvents registers all default events shipped
func (m *Measurer) RegisterDefaultEvents() (*Measurer, error) {
	return m.RegisterEvents([]*sources.Event{
		{
			Name:          "Pod Created",
			Metric:        "pod_created",
			SrcName:       k8ssrc.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(k8ssrc.Name)).(*k8ssrc.Source).FindPodCreationTime(),
		},
		{
			Name:          "Fleet Requested",
			Metric:        "fleet_requested",
			SrcName:       ec2src.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(ec2src.Name)).(*ec2src.Source).FindFleetStart(),
		},
		{
			Name:          "Instance Pending",
			Metric:        "instance_pending",
			SrcName:       imdssrc.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(imdssrc.Name)).(*imdssrc.Source).FindByPath(imdssrc.PendingTime),
		},
		{
			Name:          "VM Initialized",
			Metric:        "vm_initialized",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(vmInit),
		},
		{
			Name:          "Network Start",
			Metric:        "network_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(networkStart),
		},
		{
			Name:          "Network Ready",
			Metric:        "network_ready",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(networkReady),
		},
		{
			Name:          "Cloud-Init Initial Start",
			Metric:        "cloudinit_initial_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(cloudInitInitialStart),
		},
		{
			Name:          "Cloud-Init Config Start",
			Metric:        "cloudinit_config_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(cloudInitConfigStart),
		},
		{
			Name:          "Cloud-Init Final Start",
			Metric:        "cloudinit_final_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(cloudInitFinalStart),
		},
		{
			Name:          "Cloud-Init Final Finish",
			Metric:        "cloudinit_final_finish",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(cloudInitFinalFinish),
		},
		{
			Name:          "Containerd Start",
			Metric:        "conatinerd_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(containerdStart),
		},
		{
			Name:          "Containerd Initialized",
			Metric:        "conatinerd_initialized",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(containerdInitialized),
		},
		{
			Name:          "Kubelet Start",
			Metric:        "kubelet_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(kubeletStart),
		},
		{
			Name:          "Kubelet Initialized",
			Metric:        "kubelet_initialized",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(kubeletInitialized),
		},
		{
			Name:          "Kubelet Registered",
			Metric:        "kubelet_registered",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(kubeletRegistered),
		},
		{
			Name:          "Kube-Proxy Start",
			Metric:        "kube_proxy_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(kubeProxyStart),
		},
		{
			Name:          "VPC CNI Init Start",
			Metric:        "vpc_cni_init_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(vpcCNIInitStart),
		},
		{
			Name:          "AWS Node Start",
			Metric:        "aws_node_start",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(awsNodeStart),
		},
		{
			Name:          "VPC CNI Plugin Initialized",
			Metric:        "vpc_cni_plugin_initialized",
			SrcName:       awsnode.Name,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(awsnode.Name)).(*awsnode.Source).FindByRegex(vpcCNIInitialized),
		},
		{
			Name:          "Kube-APIServer Throttled",
			Metric:        "kube_apiserver_throttled",
			SrcName:       messages.Name,
			MatchSelector: sources.EventMatchSelectorAll,
			CommentFn:     sources.CommentMatchedLine(),
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(throttled),
		},
		{
			Name:          "Node Ready",
			Metric:        "node_ready",
			SrcName:       messages.Name,
			Terminal:      true,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(nodeReady),
		},
		{
			Name:          "Pod Ready",
			Metric:        "pod_ready",
			SrcName:       messages.Name,
			Terminal:      true,
			MatchSelector: sources.EventMatchSelectorFirst,
			FindFn:        lo.Must(m.GetSource(messages.Name)).(*messages.Source).FindByRegex(regexp.MustCompile(fmt.Sprintf(podReadyStr, m.podNamespace))),
		},
	}...)
}
