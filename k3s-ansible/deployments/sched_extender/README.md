# Multi-Resource Kubernetes Scheduler Extender

This project implements a Kubernetes scheduler extender that makes scheduling decisions based on multiple resource dimensions, including CPU, memory, disk I/O, and network bandwidth. It uses Prometheus metrics to track resource usage and availability on nodes.

## Features

- Takes into account extended resource metrics beyond CPU and memory
- Uses Prometheus metrics to track dynamic resource usage
- Implements both filtering and priority functions for the Kubernetes scheduler
- Exposes an HTTP API compatible with the Kubernetes scheduler extender interface

## Prerequisites

- Kubernetes cluster
- Prometheus deployed with node_exporter on all nodes
- Docker

## Getting Started

### Building the Scheduler Extender

```bash
docker build -t multi-resource-scheduler:latest .
```

### Deploying to Kubernetes

1. Deploy the scheduler extender:

```bash
kubectl apply -f k8s/deployment.yaml
```

2. Configure the Kubernetes scheduler to use the extender:

```bash
kubectl apply -f k8s/scheduler-config.yaml
```

3. Restart the Kubernetes scheduler to apply the configuration:

```bash
kubectl -n kube-system delete pod -l component=kube-scheduler
```

## Configuration

The scheduler extender accepts the following environment variables:

- `PROMETHEUS_URL`: URL of the Prometheus server (default: http://prometheus-server.monitoring.svc.cluster.local:9090)
- `MULTIRESOURCE_ALPHA`: Alpha parameter for the scheduling algorithm (default: 0.8)
- `MULTIRESOURCE_MAXSCORE`: Maximum score for the priority function (default: 100)

## How it Works

The scheduler extender implements two key functions:

1. **Filter**: Determines if a node has sufficient resources to run a pod
2. **Prioritize**: Scores nodes based on their resource availability

The extender periodically queries Prometheus to update its node stats cache, which contains information about CPU, memory, disk I/O, and network bandwidth availability.

## API Endpoints

- `/filter`: Implements the filter function
- `/prioritize`: Implements the prioritize function
- `/health`: Health check endpoint

## Development

The project is structured as follows:

- `pkg/multiresource/`: Core scheduling logic and types
- `main.go`: HTTP server entry point
- `k8s/`: Kubernetes deployment manifests

To run the extender locally:

```bash
go run main.go --port=8888
```

## Performance Considerations

The scheduler extender is designed to provide better scheduling decisions by considering multiple resource dimensions. While this adds some latency to the scheduling process, the benefits of improved resource allocation often outweigh the costs, especially in environments with resource-intensive workloads.

The extender caches node statistics to minimize the impact on scheduling latency. 