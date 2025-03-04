#!/usr/bin/env python3
"""
Evaluation script for comparing the performance of the default Kubernetes scheduler 
with the Kubernetes scheduler enhanced by the scheduler extender.
"""

import os
import json
import time
import subprocess
import matplotlib.pyplot as plt
import numpy as np
from datetime import datetime

# Create results directory
RESULTS_DIR = "results"
os.makedirs(RESULTS_DIR, exist_ok=True)

def run_command(cmd):
    """Run a shell command and return the output"""
    result = subprocess.run(cmd, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return result.stdout.decode('utf-8')

def get_node_metrics():
    """Get resource metrics from all nodes"""
    node_metrics = {}
    
    # Get all nodes
    nodes_json = run_command("kubectl get nodes -o json")
    nodes = json.loads(nodes_json)["items"]
    
    for node in nodes:
        node_name = node["metadata"]["name"]
        
        # Get node resource capacity
        capacity = node["status"]["capacity"]
        cpu_capacity = float(capacity["cpu"])
        memory_str = capacity["memory"]
        # Convert memory string (like 8192Mi) to bytes
        if memory_str.endswith("Ki"):
            memory_capacity = float(memory_str[:-2]) * 1024
        elif memory_str.endswith("Mi"):
            memory_capacity = float(memory_str[:-2]) * 1024 * 1024
        elif memory_str.endswith("Gi"):
            memory_capacity = float(memory_str[:-2]) * 1024 * 1024 * 1024
        else:
            memory_capacity = float(memory_str)
        
        node_metrics[node_name] = {
            "cpu_capacity": cpu_capacity,
            "memory_capacity": memory_capacity,
            "cpu_used": 0,
            "memory_used": 0,
            "pods": []
        }
    
    return node_metrics

def update_metrics_with_pods(node_metrics, namespace):
    """Update node metrics with pod resource usage"""
    pods_json = run_command(f"kubectl get pods -n {namespace} -o json")
    if not pods_json.strip():
        print(f"No pods found in namespace {namespace}")
        return
    
    pods = json.loads(pods_json)["items"]
    
    # Reset pod counts
    for node_name in node_metrics:
        node_metrics[node_name]["cpu_used"] = 0
        node_metrics[node_name]["memory_used"] = 0
        node_metrics[node_name]["pods"] = []
    
    for pod in pods:
        if pod["status"]["phase"] != "Running":
            continue
            
        pod_name = pod["metadata"]["name"]
        node_name = pod["spec"]["nodeName"]
        
        if node_name not in node_metrics:
            continue
            
        # Sum resource requests from all containers
        cpu_request = 0
        memory_request = 0
        
        for container in pod["spec"]["containers"]:
            if "resources" in container and "requests" in container["resources"]:
                requests = container["resources"]["requests"]
                
                if "cpu" in requests:
                    cpu_str = requests["cpu"]
                    if cpu_str.endswith("m"):
                        cpu_request += float(cpu_str[:-1]) / 1000
                    else:
                        cpu_request += float(cpu_str)
                
                if "memory" in requests:
                    mem_str = requests["memory"]
                    if mem_str.endswith("Ki"):
                        memory_request += float(mem_str[:-2]) * 1024
                    elif mem_str.endswith("Mi"):
                        memory_request += float(mem_str[:-2]) * 1024 * 1024
                    elif mem_str.endswith("Gi"):
                        memory_request += float(mem_str[:-2]) * 1024 * 1024 * 1024
                    else:
                        memory_request += float(mem_str)
        
        node_metrics[node_name]["cpu_used"] += cpu_request
        node_metrics[node_name]["memory_used"] += memory_request
        node_metrics[node_name]["pods"].append({
            "name": pod_name,
            "cpu_request": cpu_request,
            "memory_request": memory_request
        })
    
    return node_metrics

def get_scheduling_latency(namespace):
    """Get scheduling latency for pods in the namespace"""
    latencies = []
    pods_json = run_command(f"kubectl get pods -n {namespace} -o json")
    if not pods_json.strip():
        return latencies
        
    pods = json.loads(pods_json)["items"]
    
    for pod in pods:
        if pod["status"]["phase"] != "Running":
            continue
            
        creation_time = datetime.strptime(
            pod["metadata"]["creationTimestamp"],
            "%Y-%m-%dT%H:%M:%SZ"
        )
        
        # Find when pod was scheduled
        scheduled_time = None
        for condition in pod["status"].get("conditions", []):
            if condition["type"] == "PodScheduled" and condition["status"] == "True":
                scheduled_time = datetime.strptime(
                    condition["lastTransitionTime"],
                    "%Y-%m-%dT%H:%M:%SZ"
                )
                break
        
        if scheduled_time:
            # Calculate latency in seconds
            latency = (scheduled_time - creation_time).total_seconds()
            latencies.append(latency)
    
    return latencies

def visualize_resource_utilization(default_metrics, extender_metrics, test_case):
    """Create visualizations comparing resource utilization"""
    node_names = list(default_metrics.keys())
    
    # CPU utilization comparison
    fig, ax = plt.subplots(figsize=(12, 6))
    
    x = np.arange(len(node_names))
    width = 0.35
    
    default_cpu_util = [default_metrics[node]["cpu_used"] / default_metrics[node]["cpu_capacity"] * 100 
                        for node in node_names]
    extender_cpu_util = [extender_metrics[node]["cpu_used"] / extender_metrics[node]["cpu_capacity"] * 100 
                         for node in node_names]
    
    ax.bar(x - width/2, default_cpu_util, width, label='Default Scheduler')
    ax.bar(x + width/2, extender_cpu_util, width, label='Scheduler with Extender')
    
    ax.set_ylabel('CPU Utilization (%)')
    ax.set_title('CPU Utilization by Node')
    ax.set_xticks(x)
    ax.set_xticklabels(node_names, rotation=45)
    ax.legend()
    ax.grid(True, linestyle='--', alpha=0.7)
    
    plt.tight_layout()
    plt.savefig(f"{RESULTS_DIR}/test-case-{test_case}-cpu-utilization.png")
    
    # Memory utilization comparison
    fig, ax = plt.subplots(figsize=(12, 6))
    
    default_mem_util = [default_metrics[node]["memory_used"] / default_metrics[node]["memory_capacity"] * 100 
                        for node in node_names]
    extender_mem_util = [extender_metrics[node]["memory_used"] / extender_metrics[node]["memory_capacity"] * 100 
                         for node in node_names]
    
    ax.bar(x - width/2, default_mem_util, width, label='Default Scheduler')
    ax.bar(x + width/2, extender_mem_util, width, label='Scheduler with Extender')
    
    ax.set_ylabel('Memory Utilization (%)')
    ax.set_title('Memory Utilization by Node')
    ax.set_xticks(x)
    ax.set_xticklabels(node_names, rotation=45)
    ax.legend()
    ax.grid(True, linestyle='--', alpha=0.7)
    
    plt.tight_layout()
    plt.savefig(f"{RESULTS_DIR}/test-case-{test_case}-memory-utilization.png")
    
    # Save metrics to file
    with open(f"{RESULTS_DIR}/test-case-{test_case}-metrics.json", "w") as f:
        json.dump({
            "default_scheduler": default_metrics,
            "scheduler_with_extender": extender_metrics
        }, f, indent=2)

def visualize_scheduling_latency(default_latencies, extender_latencies, test_case):
    """Create visualizations comparing scheduling latency"""
    fig, ax = plt.subplots(figsize=(10, 6))
    
    # Create histogram
    ax.hist(default_latencies, alpha=0.5, label='Default Scheduler')
    ax.hist(extender_latencies, alpha=0.5, label='Scheduler with Extender')
    
    ax.set_xlabel('Latency (seconds)')
    ax.set_ylabel('Count')
    ax.set_title('Scheduling Latency Distribution')
    ax.legend()
    ax.grid(True, linestyle='--', alpha=0.7)
    
    plt.tight_layout()
    plt.savefig(f"{RESULTS_DIR}/test-case-{test_case}-latency.png")
    
    # Save latency data to file
    with open(f"{RESULTS_DIR}/test-case-{test_case}-latency.json", "w") as f:
        json.dump({
            "default_scheduler": default_latencies,
            "scheduler_with_extender": extender_latencies,
            "default_avg": sum(default_latencies) / len(default_latencies) if default_latencies else 0,
            "extender_avg": sum(extender_latencies) / len(extender_latencies) if extender_latencies else 0
        }, f, indent=2)

def calculate_dominant_share_metric(metrics):
    """
    Calculate a metric based on dominant resource share fairness.
    Lower values indicate better distribution according to dominant resource fairness.
    """
    node_scores = []
    
    for node_name, node_data in metrics.items():
        if node_data["cpu_capacity"] == 0 or node_data["memory_capacity"] == 0:
            continue
            
        cpu_share = node_data["cpu_used"] / node_data["cpu_capacity"]
        memory_share = node_data["memory_used"] / node_data["memory_capacity"]
        
        # Dominant share is the maximum of all resource shares
        dominant_share = max(cpu_share, memory_share)
        node_scores.append(dominant_share)
    
    if not node_scores:
        return 0
    
    # For fairness, we want the standard deviation of dominant shares to be low
    # Lower standard deviation means more equal allocation according to DRF
    return np.std(node_scores)

def run_test_case(test_case, default_yaml, extender_yaml):
    """Run a test case and gather metrics"""
    print(f"\n--- Running Test Case {test_case} ---")
    
    # Clean up any previous test resources
    cleanup_cmd = f"kubectl delete -f {default_yaml} --ignore-not-found=true && kubectl delete -f {extender_yaml} --ignore-not-found=true"
    run_command(cleanup_cmd)
    time.sleep(5)  # Give time for cleanup
    
    # Deploy test resources for default scheduler
    print(f"Deploying resources with default scheduler...")
    run_command(f"kubectl apply -f {default_yaml}")
    
    # Wait for pods to be scheduled and running
    print("Waiting for pods to be scheduled...")
    time.sleep(30)  # Adjust as needed
    
    # Gather metrics for default scheduler
    default_namespace = f"sched-test{'-' + test_case if test_case else ''}"
    node_metrics = get_node_metrics()
    update_metrics_with_pods(node_metrics, default_namespace)
    default_metrics = json.loads(json.dumps(node_metrics))  # Deep copy
    
    # Get scheduling latency
    default_latencies = get_scheduling_latency(default_namespace)
    
    # Calculate dominant share metric
    default_drf_metric = calculate_dominant_share_metric(default_metrics)
    print(f"Default Scheduler DRF Metric: {default_drf_metric}")
    
    # Clean up default scheduler resources
    run_command(f"kubectl delete -f {default_yaml}")
    time.sleep(20)  # Give time for cleanup
    
    # Deploy test resources for scheduler with extender
    print(f"Deploying resources to test scheduler with extender...")
    run_command(f"kubectl apply -f {extender_yaml}")
    
    # Wait for pods to be scheduled and running
    print("Waiting for pods to be scheduled...")
    time.sleep(30)  # Adjust as needed
    
    # Gather metrics for scheduler with extender
    extender_namespace = f"sched-test{'-' + test_case if test_case else ''}-extender"
    node_metrics = get_node_metrics()
    update_metrics_with_pods(node_metrics, extender_namespace)
    extender_metrics = node_metrics
    
    # Get scheduling latency
    extender_latencies = get_scheduling_latency(extender_namespace)
    
    # Calculate dominant share metric
    extender_drf_metric = calculate_dominant_share_metric(extender_metrics)
    print(f"Scheduler with Extender DRF Metric: {extender_drf_metric}")
    
    # Create visualizations
    visualize_resource_utilization(default_metrics, extender_metrics, test_case)
    visualize_scheduling_latency(default_latencies, extender_latencies, test_case)
    
    # Save summary for this test case
    summary = {
        "default_scheduler": {
            "drf_metric": default_drf_metric,
            "avg_latency": sum(default_latencies) / len(default_latencies) if default_latencies else 0
        },
        "scheduler_with_extender": {
            "drf_metric": extender_drf_metric,
            "avg_latency": sum(extender_latencies) / len(extender_latencies) if extender_latencies else 0
        }
    }
    
    return summary

def main():
    """Main function to run all test cases"""
    print("Starting Scheduler Evaluation...")
    
    # Define test cases
    test_cases = [
        {
            "name": "1",
            "description": "Basic Resource Distribution",
            "default_yaml": "test-case-1-pods.yaml",
            "extender_yaml": "test-case-1-pods-with-extender.yaml"
        },
        {
            "name": "2",
            "description": "Mixed Workload",
            "default_yaml": "test-case-2-mixed-workload.yaml",
            "extender_yaml": "test-case-2-mixed-workload-with-extender.yaml"
        },
        {
            "name": "3",
            "description": "Scaling Test",
            "default_yaml": "test-case-3-scaling.yaml",
            "extender_yaml": "test-case-3-scaling-with-extender.yaml"
        }
    ]
    
    overall_summary = {}
    
    for tc in test_cases:
        summary = run_test_case(tc["name"], tc["default_yaml"], tc["extender_yaml"])
        overall_summary[tc["name"]] = {
            "description": tc["description"],
            "results": summary
        }
    
    # Save overall summary
    with open(f"{RESULTS_DIR}/overall-summary.json", "w") as f:
        json.dump(overall_summary, f, indent=2)
    
    # Generate summary text file
    with open(f"{RESULTS_DIR}/summary.txt", "w") as f:
        f.write("Scheduler Evaluation Summary\n")
        f.write("==========================\n\n")
        
        for tc_name, tc_data in overall_summary.items():
            f.write(f"Test Case {tc_name}: {tc_data['description']}\n")
            f.write("-" * (19 + len(tc_data['description'])) + "\n")
            
            default_metrics = tc_data['results']['default_scheduler']
            extender_metrics = tc_data['results']['scheduler_with_extender']
            
            f.write(f"Default Scheduler:\n")
            f.write(f"  - DRF Metric (lower is better): {default_metrics['drf_metric']:.4f}\n")
            f.write(f"  - Avg Scheduling Latency: {default_metrics['avg_latency']:.4f} seconds\n\n")
            
            f.write(f"Scheduler with Extender:\n")
            f.write(f"  - DRF Metric (lower is better): {extender_metrics['drf_metric']:.4f}\n")
            f.write(f"  - Avg Scheduling Latency: {extender_metrics['avg_latency']:.4f} seconds\n\n")
            
            # Calculate improvements
            drf_improvement = (default_metrics['drf_metric'] - extender_metrics['drf_metric']) / default_metrics['drf_metric'] * 100 if default_metrics['drf_metric'] != 0 else 0
            latency_change = (extender_metrics['avg_latency'] - default_metrics['avg_latency']) / default_metrics['avg_latency'] * 100 if default_metrics['avg_latency'] != 0 else 0
            
            f.write(f"Improvements:\n")
            if drf_improvement > 0:
                f.write(f"  - Fairness Improvement: {drf_improvement:.2f}%\n")
            else:
                f.write(f"  - Fairness Change: {drf_improvement:.2f}%\n")
                
            if latency_change <= 0:
                f.write(f"  - Latency Improvement: {-latency_change:.2f}%\n")
            else:
                f.write(f"  - Latency Overhead: {latency_change:.2f}%\n")
            
            f.write("\n\n")
        
        f.write("Overall Conclusion:\n")
        f.write("-----------------\n")
        
        # Calculate average improvements across all test cases
        avg_drf_improvement = np.mean([
            (tc_data['results']['default_scheduler']['drf_metric'] - tc_data['results']['scheduler_with_extender']['drf_metric']) / tc_data['results']['default_scheduler']['drf_metric'] * 100 
            if tc_data['results']['default_scheduler']['drf_metric'] != 0 else 0
            for tc_name, tc_data in overall_summary.items()
        ])
        
        avg_latency_change = np.mean([
            (tc_data['results']['scheduler_with_extender']['avg_latency'] - tc_data['results']['default_scheduler']['avg_latency']) / tc_data['results']['default_scheduler']['avg_latency'] * 100
            if tc_data['results']['default_scheduler']['avg_latency'] != 0 else 0
            for tc_name, tc_data in overall_summary.items()
        ])
        
        if avg_drf_improvement > 10:
            f.write("The scheduler extender shows a SIGNIFICANT improvement in resource fairness ")
        elif avg_drf_improvement > 0:
            f.write("The scheduler extender shows a MODEST improvement in resource fairness ")
        else:
            f.write("The scheduler extender does not show an improvement in resource fairness ")
            
        f.write(f"with an average improvement of {avg_drf_improvement:.2f}% in DRF metrics.\n\n")
        
        if avg_latency_change > 10:
            f.write(f"However, using the scheduler extender incurs a scheduling latency overhead of {avg_latency_change:.2f}%.\n")
        elif avg_latency_change > 0:
            f.write(f"The scheduler extender introduces a minimal latency overhead of {avg_latency_change:.2f}%.\n")
        else:
            f.write(f"Interestingly, the scheduler extender actually improves scheduling latency by {-avg_latency_change:.2f}%.\n")
    
    print("\nEvaluation complete! Results saved to the results directory.")

if __name__ == "__main__":
    main() 