# Multi-Resource Scheduler

A Kubernetes scheduler that makes scheduling decisions based on multiple resource metrics (CPU, memory, disk, network) obtained from Prometheus.

## Overview

The Multi-Resource Scheduler is a custom Kubernetes scheduler that:

- Queries Prometheus for real-time resource metrics on nodes
- Makes scheduling decisions based on multiple resource dimensions
- Schedules pods to nodes with the most available resources
- Uses a scoring mechanism to find the best node for each pod

This scheduler is based on the preemptive scheduler but does not include preemption functionality, making it suitable for environments where preemption is not desired.

## Deployment

To deploy the Multi-Resource Scheduler:

1. Ensure you have Docker and kubectl configured correctly
2. Run the deployment script:

```bash
./deploy.sh
```

This will:
- Build the Docker image
- Push it to the specified registry
- Apply the Kubernetes deployment

## Configuration

The scheduler accepts the following command-line arguments:

- `--node-name`: The name of the node to schedule pods on (default: all nodes)
- `--prometheus-url`: The URL of the Prometheus server (default: http://prometheus-k8s.monitoring.svc:9090)
- `--poll-interval`: How often to check for unscheduled pods (default: 5 seconds)
- `--cpu-request`: CPU request for each pod (default: 100m)
- `--memory-request`: Memory request for each pod (default: 100Mi)

## Monitoring

To check if the scheduler is running:

```bash
kubectl -n kube-system get pods -l app=multi-resource-scheduler
```

To view logs:

```bash
kubectl -n kube-system logs -l app=multi-resource-scheduler
```

## Differences from Preemptive Scheduler

The Multi-Resource Scheduler is identical to the Preemptive Scheduler except:

1. It does not include preemption functionality
2. It will not evict lower-priority pods to make room for higher-priority pods
3. It only schedules pods when resources are available 