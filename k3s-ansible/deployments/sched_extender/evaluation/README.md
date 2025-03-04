# Multi-Resource Scheduler Extender Evaluation

This directory contains tools and resources for evaluating the performance and effectiveness of the multi-resource scheduler extender compared to the default Kubernetes scheduler.

## Overview

The multi-resource scheduler extender implements a dominant resource fairness (DRF) approach to multi-resource scheduling, which aims to optimize resource allocation by considering not just CPU and memory but also disk and network I/O metrics. This evaluation toolkit helps assess whether this approach provides measurable benefits over the default Kubernetes scheduler.

## Contents

- `test-pod-with-io-annotations.yaml`: Example YAML files demonstrating how to annotate pods with disk and network I/O requirements.
- `test-scheduler-performance.sh`: A script to evaluate and compare the performance of the multi-resource scheduler with the default scheduler.

## Evaluation Metrics

The evaluation focuses on several key metrics:

1. **Scheduling Efficiency**: How quickly and efficiently pods are scheduled.
2. **Resource Utilization**: How evenly resources are utilized across nodes.
3. **Fairness**: Whether all applications receive their fair share of resources.
4. **Stability**: Whether the scheduler makes consistent decisions over time.

## Setting Up I/O Annotations

To leverage the multi-resource scheduling capabilities, add annotations to your pod or deployment specifications:

```yaml
annotations:
  # Disk read: 30 MB/s
  scheduler.extender/disk-read: "30M"
  # Disk write: 15 MB/s
  scheduler.extender/disk-write: "15M"
  # Network upload: 5 MB/s
  scheduler.extender/net-up: "5M"
  # Network download: 10 MB/s
  scheduler.extender/net-down: "10M"
```

For pods without explicit annotations, the scheduler will estimate I/O requirements based on the container image and resource requests.

## Using the Scheduler

To use the multi-resource scheduler, add the `schedulerName` field to your pod specification:

```yaml
spec:
  schedulerName: multiresource-scheduler
  containers:
  # ...
```

## Running the Evaluation

1. Make sure your Kubernetes cluster is running and the scheduler extender is deployed.
2. Make the evaluation script executable:

   ```bash
   chmod +x test-scheduler-performance.sh
   ```

3. Run the evaluation script:

   ```bash
   ./test-scheduler-performance.sh
   ```

The script will:
- Test different types of workloads (CPU/memory intensive, I/O intensive, mixed)
- Compare scheduling times between the default scheduler and multi-resource scheduler
- Show the distribution of pods across nodes
- Calculate performance improvements

## Interpreting the Results

### Scheduling Time

Faster scheduling times indicate more efficient decision-making by the scheduler. However, this is just one metric and should be considered alongside others.

### Pod Distribution

A more even distribution of pods across nodes (especially considering their resource requirements) indicates better resource utilization and fairness.

### Resource Utilization

If Prometheus is available in your cluster, you can observe resource utilization metrics to see if the multi-resource scheduler leads to more balanced resource usage across your cluster.

## Advanced Evaluation

For more comprehensive evaluation:

1. **Long-running tests**: Run workloads for extended periods to observe stability.
2. **Varied workloads**: Test with a diverse mix of application types.
3. **Resource pressure**: Introduce resource constraints to test scheduler behavior under pressure.
4. **Custom metrics**: Define application-specific performance metrics.

## Integration with Prometheus

If you have Prometheus deployed in your cluster, you can use it to collect and visualize resource utilization metrics. Key metrics to observe include:

- CPU utilization per node
- Memory utilization per node
- Disk I/O rates per node
- Network I/O rates per node

These metrics can provide deeper insights into the effectiveness of the multi-resource scheduler in balancing resource usage across the cluster.

## Known Limitations

- The scheduler currently assumes that all nodes have similar I/O capabilities. Future versions may incorporate node-specific I/O capacity information.
- Estimated I/O requirements for pods without annotations are based on general patterns and may not accurately reflect actual I/O needs for all applications.
- The current implementation focuses on fairness rather than absolute performance optimization. 