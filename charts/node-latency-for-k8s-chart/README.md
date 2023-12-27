# node-latency-for-k8s-chart

A Helm chart for node-latency-for-k8s tooling

![Version: 0.1.8](https://img.shields.io/badge/Version-0.1.8-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.8](https://img.shields.io/badge/AppVersion-0.1.8-informational?style=flat-square)

## Documentation

For full node-latency-for-k8s documentation please checkout [https://github.com/awslabs/node-latency-for-k8s](https://github.com/awslabs/node-latency-for-k8s).

## Installing the Chart

```bash
helm upgrade --install --namespace node-latency-for-k8s --create-namespace \
  node-latency-for-k8s oci://public.ecr.aws/eks-nodes/node-latency-for-k8s-chart \
  --version v0.1.8 \
  --set serviceAccount.annotations.eks\.amazonaws\.com/role-arn=${NLK_IAM_ROLE_ARN} \
  --wait
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| env[0].name | string | `"PROMETHEUS_METRICS"` |  |
| env[0].value | string | `"true"` |  |
| env[1].name | string | `"CLOUDWATCH_METRICS"` |  |
| env[1].value | string | `"false"` |  |
| env[2].name | string | `"OTEL_METRICS"` |  |
| env[2].value | string | `"false"` |  |
| env[3].name | string | `"OUTPUT"` |  |
| env[3].value | string | `"markdown"` |  |
| env[4].name | string | `"NO_COMMENTS"` |  |
| env[4].value | string | `"false"` |  |
| env[5].name | string | `"TIMEOUT"` |  |
| env[5].value | string | `"300"` |  |
| env[6].name | string | `"POD_NAMESPACE"` |  |
| env[6].value | string | `"default"` |  |
| env[7].name | string | `"NODE_NAME"` |  |
| env[7].valueFrom.fieldRef.fieldPath | string | `"spec.nodeName"` |  |
| fullnameOverride | string | `""` |  |
| image.digest | string | `"sha256:a47a43d734f65ff3907950a21a0afbbd2056830465dffde701455a09e871a6b0"` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"public.ecr.aws/g4k0u1s2/node-latency-for-k8s"` |  |
| image.tag | string | `"v0.1.8"` |  |
| nameOverride | string | `""` |  |
| nodeSelector."kubernetes.io/arch" | string | `"amd64"` |  |
| nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| podAnnotations | object | `{}` |  |
| podMonitor.create | bool | `false` |  |
| podSecurityContext.fsGroup | int | `0` |  |
| podSecurityContext.runAsGroup | int | `0` |  |
| podSecurityContext.runAsUser | int | `0` |  |
| priorityClassName | string | `""` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"200m"` |  |
| resources.requests.memory | string | `"256Mi"` |  |
| securityContext.capabilities | object | `{}` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `"node-latency-for-k8s"` |  |
| tolerations | list | `[]` |  |

