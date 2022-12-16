# node-latency-for-k8s-chart

A Helm chart for node-latency-for-k8s tooling

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

## Documentation

For full node-latency-for-k8s documentation please checkout [https://github.com/awslabs/node-latency-for-k8s](https://github.com/awslabs/node-latency-for-k8s).

## Installing the Chart

```bash
helm upgrade --install --namespace node-latency-for-k8s --create-namespace \
  node-latency-for-k8s oci://public.ecr.aws/eks-nodes/node-latency-for-k8s-chart \
  --version v0.1.0 \
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
| env[1].value | string | `"true"` |  |
| env[2].name | string | `"OUTPUT"` |  |
| env[2].value | string | `"markdown"` |  |
| fullnameOverride | string | `""` |  |
| image.digest | string | `"public.ecr.aws/m0e9w1v1/node-latency-for-k8s:v0.1.0@sha256:19fd0503ca8058490a5b97e30e63970b1e268e2d6f09e334a49326664595a7df"` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"public.ecr.aws/m0e9w1v1/node-latency-for-k8s"` |  |
| image.tag | string | `""` |  |
| nameOverride | string | `""` |  |
| nodeSelector."kubernetes.io/arch" | string | `"amd64"` |  |
| nodeSelector."kubernetes.io/os" | string | `"linux"` |  |
| podAnnotations | object | `{}` |  |
| podMonitor.create | bool | `false` |  |
| podSecurityContext.fsGroup | int | `0` |  |
| podSecurityContext.runAsGroup | int | `0` |  |
| podSecurityContext.runAsUser | int | `0` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| resources.requests.cpu | string | `"200m"` |  |
| resources.requests.memory | string | `"256Mi"` |  |
| securityContext.capabilities | object | `{}` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `""` |  |
| tolerations | list | `[]` |  |

