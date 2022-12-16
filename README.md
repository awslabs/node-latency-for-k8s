# Node-Latency-for-K8s (NLK)

The node-latency-for-k8s tool analyzes logs on a K8s node and outputs a timing chart, cloudwatch metrics, prometheus metrics, and/or json timing data. This tool is intended to analyze the components that contribute to the node launch latency so that they can be optimized to bring nodes online faster. 

NLK runs as a stand-alone binary that can be executed on a node or on offloaded node logs. It can also be run as a K8s DaemonSet to large-scale node latency measurements in a standardized and extensible way. 


## Usage:

```
> node-latency-for-k8s --help
Usage for node-latency-for-k8s:

 Flags:
   --cloudwatch-metrics
      Emit metrics to CloudWatch, default: false
   --experiment-dimension
      Custom dimension to add to experiment metrics, default: none
   --imds-endpoint
      IMDS endpoint for testing, default: http://169.254.169.254
   --metrics-port
      The port to serve prometheus metrics from, default: 2112
   --no-imds
      Do not use EC2 Instance Metadata Service (IMDS), default: false
   --output
      output type (markdown or json), default: markdown
   --prometheus-metrics
      Expose a Prometheus metrics endpoint (this runs as a daemon), default: false
   --retry-delay
      Delay in seconds in-between timing retrievals, default: 5
   --timeout
      Timeout in seconds for how long event timings will try to be retrieved, default: 600
   --version
      version information
```

## Examples:

### Example 1 - Chart

```
> node-latency-for-k8s --output markdown
### i-06428b3f38ba5ec42 (10.0.1.16) | c6a.large | x86_64 | us-east-2c | ami-0bdaaba680b3688e2
|           EVENT            |      TIMESTAMP       |  T  |
|----------------------------|----------------------|-----|
| Instance Requested         | 2022-12-12T02:20:01Z | 0s  |
| Instance Pending           | 2022-12-12T02:20:02Z | 1s  |
| VM Initialized             | 2022-12-12T02:20:12Z | 11s |
| Containerd Initialized     | 2022-12-12T02:20:16Z | 15s |
| Network Start              | 2022-12-12T02:20:16Z | 15s |
| Cloud-Init Initial Start   | 2022-12-12T02:20:16Z | 15s |
| Network Ready              | 2022-12-12T02:20:16Z | 15s |
| Containerd Start           | 2022-12-12T02:20:16Z | 15s |
| Cloud-Init Config Start    | 2022-12-12T02:20:17Z | 16s |
| Cloud-Init Final Start     | 2022-12-12T02:20:18Z | 17s |
| Kubelet Initialized        | 2022-12-12T02:20:20Z | 19s |
| Kubelet Start              | 2022-12-12T02:20:20Z | 19s |
| Cloud-Init Final Finish    | 2022-12-12T02:20:20Z | 19s |
| Kubelet Registered         | 2022-12-12T02:20:20Z | 19s |
| Kube-Proxy Start           | 2022-12-12T02:20:22Z | 21s |
| VPC CNI Init Start         | 2022-12-12T02:20:22Z | 21s |
| AWS Node Start             | 2022-12-12T02:20:22Z | 21s |
| VPC CNI Plugin Initialized | 2022-12-12T02:20:24Z | 23s |
| Pod Ready                  | 2022-12-12T02:20:30Z | 29s |
| Node Ready                 | 2022-12-12T02:20:31Z | 30s |
```

## Extensibility

The node-latency-for-k8s tool is written in go and exposes a package called `latency` and `sources` that can be used to extend NLK with more sources and events. The default sources NLK loads are:

1. messages - `/var/log/messages*`
2. aws-node - `/var/log/pods/kube-system_aws-node-*/aws-node/*.log`
3. imds - `http://169.254.169.254`

There is also a generic `LogReader` struct that is used by the `messages` and the `aws-node` sources which makes implementing other log sources trivial. Sources do not need to be log files though. The `imds` source queries the EC2 Instance Metadata Service (IMDS) to pull the EC2 Pending Time. Custom sources are able to be registered directly to the `latency` package so that sources do not have to be contributed back, but are obviously welcomed.

Additional Events can be registered to the default sources as well.

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This project is licensed under the Apache-2.0 License.
