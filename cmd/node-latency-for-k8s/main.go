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

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/awslabs/node-latency-for-k8s/pkg/latency"
)

const oneShotExcludeLabel = "node-latency-for-k8s-exclude-pod"

var (
	version string
	commit  string
)

type Options struct {
	CloudWatch          bool
	Prometheus          bool
	OTeLMetrics         bool
	ExperimentDimension string
	TimeoutSeconds      int
	RetryDelaySeconds   int
	MetricsPort         int
	OTeLEndpoint        string
	IMDSEndpoint        string
	Kubeconfig          string
	PodNamespace        string
	NodeName            string
	NoIMDS              bool
	Output              string
	NoComments          bool
	Version             bool
}

//nolint:gocyclo
func main() {
	root := flag.NewFlagSet(path.Base(os.Args[0]), flag.ExitOnError)
	root.Usage = HelpFunc(root)
	options := MustParseFlags(root)
	if options.Version {
		fmt.Printf("%s\n", version)
		fmt.Printf("Git Commit: %s\n", commit)
		os.Exit(0)
	}
	ctx := context.Background()
	var err error
	var clientset *kubernetes.Clientset
	latencyClient := latency.New()

	// Setup K8s clientset
	var k8sConfig *rest.Config
	if options.Kubeconfig != "" {
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", options.Kubeconfig)
		if err != nil {
			log.Fatalf("Unable to create K8s clientset from kubeconfig: %s", err)
		}
	} else {
		k8sConfig, err = rest.InClusterConfig()
	}
	if err == nil {
		clientset, err = kubernetes.NewForConfig(k8sConfig)
		if err != nil {
			log.Fatalf("Unable to create K8s clientset: %s", err)
		}
		latencyClient = latencyClient.WithK8sClientset(clientset).WithPodNamespace(options.PodNamespace).WithNodeName(options.NodeName)
	} else {
		log.Printf("Unable to find in-cluster K8s config: %s\n", err)
	}

	// Setup AWS Config and Clients
	cfg, err := config.LoadDefaultConfig(ctx, withIMDSEndpoint(options.IMDSEndpoint))
	if err != nil {
		log.Fatalf("unable to load AWS SDK config, %s", err)
	}
	if !options.NoIMDS {
		latencyClient = latencyClient.WithIMDS(imds.NewFromConfig(cfg))
	}
	latencyClient = latencyClient.WithEC2Client(ec2.NewFromConfig(cfg))

	// Register the Default Sources and Events
	latencyClient, err = latencyClient.RegisterDefaultSources().RegisterDefaultEvents()
	if err != nil {
		log.Println("Unable to instantiate the latency timing client: ")
		log.Printf("    %s", err)
	}

	// Take measurements
	measurement, err := latencyClient.MeasureUntil(ctx, time.Duration(options.TimeoutSeconds)*time.Second, time.Duration(options.RetryDelaySeconds)*time.Second)
	if err != nil {
		log.Println(err)
	}

	// Emit Measurement to stdout based on output type
	switch options.Output {
	case "json":
		jsonMeasurement, err := json.MarshalIndent(measurement, "", "    ")
		if err != nil {
			log.Printf("unable to marshal json output: %v", err)
		} else {
			fmt.Println(string(jsonMeasurement))
		}
	default:
		fallthrough
	case "markdown":
		var hiddenColumns []string
		if options.NoComments {
			hiddenColumns = append(hiddenColumns, latency.ChartColumnComment)
		}
		measurement.Chart(latency.ChartOptions{HiddenColumns: hiddenColumns})
	}

	// Emit CloudWatch Metrics if flag is enabled
	if options.CloudWatch {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Fatalf("unable to load AWS SDK config, %s", err)
		}
		cw := cloudwatch.NewFromConfig(cfg)
		if err := measurement.EmitCloudWatchMetrics(ctx, cw, options.ExperimentDimension); err != nil {
			log.Printf("Error emitting CloudWatch metrics: %s\n", err)
		} else {
			log.Println("Successfully emitted CloudWatch metrics")
		}
	}

	// Serve Prometheus Metrics if flag is enabled
	if options.Prometheus {
		registry := prometheus.NewRegistry()
		measurement.RegisterMetrics(registry, options.ExperimentDimension)
		http.Handle("/metrics", promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{EnableOpenMetrics: false},
		))
		log.Printf("Serving Prometheus metrics on :%d", options.MetricsPort)
		srv := &http.Server{
			ReadTimeout:       1 * time.Second,
			WriteTimeout:      1 * time.Second,
			IdleTimeout:       30 * time.Second,
			ReadHeaderTimeout: 2 * time.Second,
			Addr:              fmt.Sprintf(":%d", options.MetricsPort),
		}
		lo.Must0(srv.ListenAndServe())
	}

	// Serve OTeL metrics if flag is enabled
	if options.OTeLMetrics {
		oTel, err := measurement.RegisterOTeLMetrics(ctx, options.ExperimentDimension, version, options.OTeLEndpoint)
		if err != nil {
			log.Fatalf("error registering OTeL metrics: %s", err)
		}

		if err := oTel.SendMetrics(); err != nil {
			log.Fatalf("unable to emit OTeL metrics: %s", err)
		}

		// label the current node we're running on so it doesn't get scheduled again
		if options.NodeName != "" {
			log.Printf("Running in one-shot mode. Patching node: %s\n", options.NodeName)
			_, err = clientset.CoreV1().Nodes().Patch(context.TODO(), options.NodeName, types.MergePatchType,
				[]byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":""}}}`, oneShotExcludeLabel)),
				metav1.PatchOptions{})
			if err != nil {
				log.Fatalf("error patching node: %v", err)
			}
		}

	}

}

func MustParseFlags(f *flag.FlagSet) Options {
	options := Options{}
	f.BoolVar(&options.CloudWatch, "cloudwatch-metrics", boolEnv("CLOUDWATCH_METRICS", false), "Emit metrics to CloudWatch, default: false")
	f.BoolVar(&options.Prometheus, "prometheus-metrics", boolEnv("PROMETHEUS_METRICS", false), "Expose a Prometheus metrics endpoint (this runs as a daemon), default: false")
	f.BoolVar(&options.OTeLMetrics, "otel-metrics", boolEnv("OTEL_METRICS", false), "Collect metrics and emit once to OTeL collector")
	f.IntVar(&options.MetricsPort, "metrics-port", intEnv("METRICS_PORT", 2112), "The port to serve prometheus metrics from, default: 2112")
	f.StringVar(&options.ExperimentDimension, "experiment-dimension", strEnv("EXPERIMENT_DIMENSION", "none"), "Custom dimension to add to experiment metrics, default: none")
	f.IntVar(&options.TimeoutSeconds, "timeout", intEnv("TIMEOUT", 600), "Timeout in seconds for how long event timings will try to be retrieved, default: 600")
	f.IntVar(&options.RetryDelaySeconds, "retry-delay", intEnv("RETRY_DELAY", 5), "Delay in seconds in-between timing retrievals, default: 5")
	f.StringVar(&options.IMDSEndpoint, "imds-endpoint", strEnv("IMDS_ENDPOINT", "http://169.254.169.254"), "IMDS endpoint for testing, default: http://169.254.169.254")
	f.BoolVar(&options.NoIMDS, "no-imds", boolEnv("NO_IMDS", false), "Do not use EC2 Instance Metadata Service (IMDS), default: false")
	f.StringVar(&options.OTeLEndpoint, "otel-endpoint", strEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""), "OTeL backend endpoint for receiving metrics, default: <auto-discovered via kubernetes Downward API>")
	f.StringVar(&options.PodNamespace, "pod-namespace", strEnv("POD_NAMESPACE", "default"), "namespace of the pods that will be measured from creation to running, default: default")
	f.StringVar(&options.NodeName, "node-name", strEnv("NODE_NAME", ""), "node name to query for the first pod creation time in the pod namespace, default: <auto-discovered via IMDS>")
	f.StringVar(&options.Output, "output", strEnv("OUTPUT", "markdown"), "output type (markdown or json), default: markdown")
	f.BoolVar(&options.NoComments, "no-comments", boolEnv("NO_COMMENTS", false), "Hide the comments column in the markdown chart output, default: false")
	f.BoolVar(&options.Version, "version", false, "version information")
	f.StringVar(&options.Kubeconfig, "kubeconfig", defaultKubeconfig(), "(optional) absolute path to the kubeconfig file")
	lo.Must0(f.Parse(os.Args[1:]))
	return options
}

func HelpFunc(f *flag.FlagSet) func() {
	return func() {
		fmt.Printf("Usage for %s:\n\n", filepath.Base(os.Args[0]))
		fmt.Println(" Flags:")
		f.VisitAll(func(fl *flag.Flag) {
			fmt.Printf("   --%s\n", fl.Name)
			fmt.Printf("      %s\n", fl.Usage)
		})
	}
}

func defaultKubeconfig() string {
	if val, ok := os.LookupEnv("KUBECONFIG"); ok {
		return val
	}
	if home := homedir.HomeDir(); home != "" {
		kubeconfigPath := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(kubeconfigPath); err == nil {
			return kubeconfigPath
		}
	}
	return ""
}

// strEnv retrieves the env var key or defaults to fallback value
func strEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		if value != "" {
			return value
		}
	}
	return fallback
}

// intEnv parses env var to an int if the key exists
// panics if a parse error occurs
func intEnv(key string, fallback int) int {
	envStrValue := strEnv(key, "")
	if envStrValue == "" {
		return fallback
	}
	envIntValue, err := strconv.Atoi(envStrValue)
	if err != nil {
		panic("Env Var " + key + " must be an integer")
	}
	return envIntValue
}

// boolEnv parses env var to a boolean if the key exists
// panics if the string cannot be parsed to a boolean
// nolint:unparam
func boolEnv(key string, fallback bool) bool {
	envStrValue := strEnv(key, "")
	if envStrValue == "" {
		return fallback
	}
	envBoolValue, err := strconv.ParseBool(envStrValue)
	if err != nil {
		panic("Env Var " + key + " must be either true or false")
	}
	return envBoolValue
}

func withIMDSEndpoint(imdsEndpoint string) func(*config.LoadOptions) error {
	return func(lo *config.LoadOptions) error {
		lo.EC2IMDSEndpoint = imdsEndpoint
		lo.EC2IMDSEndpointMode = imds.EndpointModeStateIPv4
		if net.ParseIP(imdsEndpoint).To4() == nil {
			lo.EC2IMDSEndpointMode = imds.EndpointModeStateIPv6
		}
		return nil
	}
}
