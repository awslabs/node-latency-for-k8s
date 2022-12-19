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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samber/lo"

	"github.com/awslabs/node-latency-for-k8s/pkg/latency"
)

var (
	version string
	commit  string
)

type Options struct {
	CloudWatch          bool
	Prometheus          bool
	ExperimentDimension string
	TimeoutSeconds      int
	RetryDelaySeconds   int
	MetricsPort         int
	IMDSEndpoint        string
	NoIMDS              bool
	Output              string
	Version             bool
}

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
	latencyClient := latency.New()

	if !options.NoIMDS {
		cfg, err := config.LoadDefaultConfig(ctx, withIMDSEndpoint(options.IMDSEndpoint))
		if err != nil {
			log.Fatalf("unable to load AWS SDK config, %v", err)
		}
		imdsClient := imds.NewFromConfig(cfg)
		if err != nil {
			log.Fatalln(err)
		}
		latencyClient.WithIMDS(imdsClient)
	}

	latencyClient, err := latencyClient.RegisterDefaultSources().RegisterDefaultEvents()
	if err != nil {
		log.Println("Unable to instantiate the latency timing client: ")
		log.Printf("    %v", err)
	}
	measurement := latencyClient.MeasureUntil(ctx, time.Duration(options.TimeoutSeconds)*time.Second, time.Duration(options.RetryDelaySeconds)*time.Second)

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
		measurement.Chart()
	}

	// Emit CloudWatch Metrics if flag is enabled
	if options.CloudWatch {
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Fatalf("unable to load AWS SDK config, %v", err)
		}
		cw := cloudwatch.NewFromConfig(cfg)
		if err := measurement.EmitCloudWatchMetrics(ctx, cw, options.ExperimentDimension); err != nil {
			log.Printf("Error emitting CloudWatch metrics: %v\n", err)
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
}

func MustParseFlags(f *flag.FlagSet) Options {
	options := Options{}
	f.BoolVar(&options.CloudWatch, "cloudwatch-metrics", boolEnv("CLOUDWATCH_METRICS", false), "Emit metrics to CloudWatch, default: false")
	f.BoolVar(&options.Prometheus, "prometheus-metrics", boolEnv("PROMETHEUS_METRICS", false), "Expose a Prometheus metrics endpoint (this runs as a daemon), default: false")
	f.IntVar(&options.MetricsPort, "metrics-port", intEnv("METRICS_PORT", 2112), "The port to serve prometheus metrics from, default: 2112")
	f.StringVar(&options.ExperimentDimension, "experiment-dimension", strEnv("EXPERIMENT_DIMENSION", "none"), "Custom dimension to add to experiment metrics, default: none")
	f.IntVar(&options.TimeoutSeconds, "timeout", intEnv("TIMEOUT", 600), "Timeout in seconds for how long event timings will try to be retrieved, default: 600")
	f.IntVar(&options.RetryDelaySeconds, "retry-delay", intEnv("RETRY_DELAY", 5), "Delay in seconds in-between timing retrievals, default: 5")
	f.StringVar(&options.IMDSEndpoint, "imds-endpoint", strEnv("IMDS_ENDPOINT", "http://169.254.169.254"), "IMDS endpoint for testing, default: http://169.254.169.254")
	f.BoolVar(&options.NoIMDS, "no-imds", boolEnv("NO_IMDS", false), "Do not use EC2 Instance Metadata Service (IMDS), default: false")
	f.StringVar(&options.Output, "output", strEnv("OUTPUT", "markdown"), "output type (markdown or json), default: markdown")
	f.BoolVar(&options.Version, "version", false, "version information")
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
