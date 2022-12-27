# Node-Latency-for-K8s (NLK)

The node-latency-for-k8s tool analyzes logs on a K8s node and outputs a timing chart, cloudwatch metrics, prometheus metrics, and/or json timing data. This tool is intended to analyze the components that contribute to the node launch latency so that they can be optimized to bring nodes online faster. 

NLK runs as a stand-alone binary that can be executed on a node or on offloaded node logs. It can also be run as a K8s DaemonSet to perform large-scale node latency measurements in a standardized and extensible way. 


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

## Installation

### K8s DaemonSet (Helm)

**The below installation works if you do not need CloudWatch metrics**
```
export VERSION="v0.1.1"

docker logout public.ecr.aws
helm update --install node-latency-for-k8s oci://public.ecr.aws/g4k0u1s2/node-latency-for-k8s-chart \
   --version ${VERSION} \
   --namespace node-latency-for-k8s \
   --wait
```


**If you do need CloudWatch metrics, then give the DS an IAM Role**
```
export CLUSTER_NAME=<Fill in CLUSTER_NAME here>
export VERSION="v0.1.1"

export AWS_ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
export KNL_IAM_ROLE_ARN="arn:aws:iam::${AWS_ACCOUNT_ID}:role/node-latency-for-k8s-${CLUSTER_NAME}"

docker logout public.ecr.aws
helm upgrade --install node-latency-for-k8s oci://public.ecr.aws/g4k0u1s2/node-latency-for-k8s-chart \
   --version ${VERSION} \
   --namespace node-latency-for-k8s \
   --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=${KNL_IAM_ROLE_ARN} \
   --wait
```

### RPM / Deb / Binary

Packages, binaries, and archives are published for all major platforms (Mac amd64/arm64 & Linux amd64/arm64):

Debian / Ubuntu:

```
[[ `uname -m` == "aarch64" ]] && ARCH="arm64" || ARCH="amd64"
wget https://github.com/awslabs/node-latency-for-k8s/releases/download/v0.1.1/node-latency-for-k8s_0.1.1_linux_${ARCH}.deb
dpkg --install node-latency-for-k8s_0.1.1_linux_${ARCH}.deb
```

RedHat:

```
[[ `uname -m` == "aarch64" ]] && ARCH="arm64" || ARCH="amd64"
rpm -i https://github.com/awslabs/node-latency-for-k8s/releases/download/v0.1.1/node-latency-for-k8s_0.1.1_linux_${ARCH}.rpm
```

Download Binary Directly:

```
[[ `uname -m` == "aarch64" ]] && ARCH="arm64" || ARCH="amd64"
OS=`uname | tr '[:upper:]' '[:lower:]'`
wget -qO- https://github.com/awslabs/node-latency-for-k8s/releases/download/v0.1.1/node-latency-for-k8s_0.1.1_${OS}_${ARCH}.tar.gz | tar xvz
chmod +x node-latency-for-k8s
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

## Example 2 - Prometheus Metrics

```
> node-latency-for-k8s --prometheus-metrics &
### i-0f5a78a8cb71c9ef9 (192.168.147.219) | c6a.large | x86_64 | us-east-2c | ami-0bf8f0f9cd3cce116
|           EVENT            |      TIMESTAMP       |  T  |
|----------------------------|----------------------|-----|
| Instance Requested         | 2022-12-27T22:25:30Z | 0s  |
| Instance Pending           | 2022-12-27T22:25:31Z | 1s  |
| VM Initialized             | 2022-12-27T22:25:43Z | 13s |
| Containerd Initialized     | 2022-12-27T22:25:46Z | 16s |
| Network Start              | 2022-12-27T22:25:46Z | 16s |
| Cloud-Init Initial Start   | 2022-12-27T22:25:46Z | 16s |
| Network Ready              | 2022-12-27T22:25:46Z | 16s |
| Containerd Start           | 2022-12-27T22:25:46Z | 16s |
| Cloud-Init Config Start    | 2022-12-27T22:25:47Z | 17s |
| Cloud-Init Final Start     | 2022-12-27T22:25:48Z | 18s |
| Kubelet Initialized        | 2022-12-27T22:25:50Z | 20s |
| Kubelet Start              | 2022-12-27T22:25:50Z | 20s |
| Cloud-Init Final Finish    | 2022-12-27T22:25:50Z | 20s |
| Kubelet Registered         | 2022-12-27T22:25:50Z | 20s |
| Kube-Proxy Start           | 2022-12-27T22:25:52Z | 22s |
| VPC CNI Init Start         | 2022-12-27T22:25:53Z | 23s |
| AWS Node Start             | 2022-12-27T22:25:53Z | 23s |
| Node Ready                 | 2022-12-27T22:25:54Z | 24s |
| VPC CNI Plugin Initialized | 2022-12-27T22:25:54Z | 25s |
| Pod Ready                  | 2022-12-27T22:26:00Z | 30s |
2022/12/27 22:33:55 Serving Prometheus metrics on :2112

> curl localhost:2112/metrics
# HELP aws_node_start
# TYPE aws_node_start gauge
aws_node_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 23
# HELP cloudinit_config_start
# TYPE cloudinit_config_start gauge
cloudinit_config_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 17
# HELP cloudinit_final_finish
# TYPE cloudinit_final_finish gauge
cloudinit_final_finish{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 20
# HELP cloudinit_final_start
# TYPE cloudinit_final_start gauge
cloudinit_final_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 18
# HELP cloudinit_initial_start
# TYPE cloudinit_initial_start gauge
cloudinit_initial_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 16
# HELP conatinerd_initialized
# TYPE conatinerd_initialized gauge
conatinerd_initialized{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 16
# HELP conatinerd_start
# TYPE conatinerd_start gauge
conatinerd_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 16
# HELP instance_pending
# TYPE instance_pending gauge
instance_pending{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 1
# HELP instance_requested
# TYPE instance_requested gauge
instance_requested{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 0
# HELP kube_proxy_start
# TYPE kube_proxy_start gauge
kube_proxy_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 22
# HELP kubelet_initialized
# TYPE kubelet_initialized gauge
kubelet_initialized{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 20
# HELP kubelet_registered
# TYPE kubelet_registered gauge
kubelet_registered{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 20
# HELP kubelet_start
# TYPE kubelet_start gauge
kubelet_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 20
# HELP network_ready
# TYPE network_ready gauge
network_ready{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 16
# HELP network_start
# TYPE network_start gauge
network_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 16
# HELP node_ready
# TYPE node_ready gauge
node_ready{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 24
# HELP pod_ready
# TYPE pod_ready gauge
pod_ready{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 30
# HELP vm_initialized
# TYPE vm_initialized gauge
vm_initialized{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 13
# HELP vpc_cni_init_start
# TYPE vpc_cni_init_start gauge
vpc_cni_init_start{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 23
# HELP vpc_cni_plugin_initialized
# TYPE vpc_cni_plugin_initialized gauge
vpc_cni_plugin_initialized{amiID="ami-0bf8f0f9cd3cce116",availabilityZone="us-east-2c",experiment="none",instanceType="c6a.large",region="us-east-2"} 24.743959121
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
