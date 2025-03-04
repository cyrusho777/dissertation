# Preemptive Scheduler for Kubernetes

This is a custom scheduler for Kubernetes that implements preemptive scheduling based on pod priorities. It can evict lower-priority pods to make room for higher-priority pods when resources are constrained.

## Prerequisites

- Kubernetes cluster with RBAC enabled
- Prometheus installed in the cluster (for node metrics)
- Docker and kubectl installed on your local machine
- Access to a container registry

## Deployment Steps

1. **Build and push the container image**

   ```bash
   # Make the deploy script executable
   chmod +x deploy.sh
   
   # Run the deployment script with your registry
   ./deploy.sh your-registry.example.com
   ```

2. **Verify the deployment**

   ```bash
   kubectl -n kube-system get pods -l app=preemptive-scheduler
   ```

3. **Check logs**

   ```bash
   kubectl -n kube-system logs -l app=preemptive-scheduler
   ```

## Configuration

The scheduler can be configured using the following command-line flags:

- `--preemption`: Enable or disable preemption (default: false)
- `--node`: Target node for scheduling (default: node1)
- `--cpu`: CPU cores required by the Pod (default: 1.0)
- `--mem`: Memory required by the Pod in bytes (default: 1Gi)
- `--priority`: Priority of the Pod (default: 10)
- `--alpha`: Utilization threshold for scheduling (default: 0.8)

You can also configure the Prometheus URL using the `PROMETHEUS_URL` environment variable.

## Troubleshooting

If the scheduler is not working as expected, check the following:

1. **Pod status**

   ```bash
   kubectl -n kube-system describe pod -l app=preemptive-scheduler
   ```

2. **RBAC permissions**

   ```bash
   kubectl auth can-i list pods --as=system:serviceaccount:kube-system:preemptive-scheduler
   kubectl auth can-i create evictions --as=system:serviceaccount:kube-system:preemptive-scheduler -n default
   ```

3. **Prometheus connectivity**

   ```bash
   # Get a shell in the scheduler pod
   kubectl -n kube-system exec -it $(kubectl -n kube-system get pod -l app=preemptive-scheduler -o name) -- sh
   
   # Test Prometheus connectivity
   wget -O- http://prometheus-server.monitoring.svc.cluster.local:9090/api/v1/query?query=up
   ``` 