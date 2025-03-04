#!/usr/bin/env python3
"""
analyze_drf.py - Analyzes the dominant resource fairness of pod distributions

This script analyzes pod placements from the evaluation test cases to determine
how well the dominant resource fairness principle is being followed by both
the default scheduler and the scheduler with the extender.
"""

import json
import os
import subprocess
import numpy as np
import matplotlib.pyplot as plt
from collections import defaultdict

# Create results directory if it doesn't exist
os.makedirs('results/drf_analysis', exist_ok=True)

def run_command(cmd):
    """Run a shell command and return its output"""
    process = subprocess.Popen(cmd, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    stdout, stderr = process.communicate()
    if process.returncode != 0:
        print(f"Error executing command: {cmd}")
        print(stderr.decode())
        return ""
    return stdout.decode()

def get_node_capacities():
    """Get the resource capacities of all nodes"""
    output = run_command("kubectl get nodes -o json")
    if not output:
        return {}
    
    nodes_data = json.loads(output)
    node_capacities = {}
    
    for node in nodes_data.get('items', []):
        name = node['metadata']['name']
        cpu_capacity = node['status']['capacity'].get('cpu', '0')
        memory_capacity = node['status']['capacity'].get('memory', '0')
        
        # Convert CPU to millicores if needed
        if not cpu_capacity.endswith('m'):
            cpu_capacity = int(float(cpu_capacity) * 1000)
        else:
            cpu_capacity = int(cpu_capacity[:-1])
            
        # Convert memory to Mi
        if memory_capacity.endswith('Ki'):
            memory_capacity = int(memory_capacity[:-2]) / 1024
        elif memory_capacity.endswith('Mi'):
            memory_capacity = int(memory_capacity[:-2])
        elif memory_capacity.endswith('Gi'):
            memory_capacity = int(memory_capacity[:-2]) * 1024
        else:
            # Assume bytes
            memory_capacity = int(memory_capacity) / (1024 * 1024)
            
        node_capacities[name] = {
            'cpu': cpu_capacity,
            'memory': memory_capacity
        }
    
    return node_capacities

def get_pod_placements(namespace):
    """Get pod placements and their resource allocations for a namespace"""
    output = run_command(f"kubectl get pods -n {namespace} -o json")
    if not output:
        return {}
    
    pods_data = json.loads(output)
    pod_placements = defaultdict(lambda: {'cpu': 0, 'memory': 0, 'pods': 0})
    
    for pod in pods_data.get('items', []):
        node_name = pod.get('spec', {}).get('nodeName')
        if not node_name or pod.get('status', {}).get('phase') != 'Running':
            continue
            
        containers = pod.get('spec', {}).get('containers', [])
        for container in containers:
            requests = container.get('resources', {}).get('requests', {})
            
            # Get CPU request
            cpu_request = requests.get('cpu', '0')
            if not cpu_request.endswith('m'):
                cpu_millicores = int(float(cpu_request) * 1000)
            else:
                cpu_millicores = int(cpu_request[:-1])
                
            # Get memory request
            memory_request = requests.get('memory', '0')
            if memory_request.endswith('Ki'):
                memory_mi = int(memory_request[:-2]) / 1024
            elif memory_request.endswith('Mi'):
                memory_mi = int(memory_request[:-2])
            elif memory_request.endswith('Gi'):
                memory_mi = int(memory_request[:-2]) * 1024
            else:
                # Assume bytes
                memory_mi = int(memory_request) / (1024 * 1024)
                
            pod_placements[node_name]['cpu'] += cpu_millicores
            pod_placements[node_name]['memory'] += memory_mi
            
        # Count this pod
        pod_placements[node_name]['pods'] += 1
    
    return pod_placements

def calculate_drf_metrics(node_capacities, pod_placements):
    """Calculate DRF metrics for the given pod placements"""
    dominant_shares = []
    cpu_shares = []
    memory_shares = []
    
    for node_name, resources in pod_placements.items():
        if node_name not in node_capacities:
            continue
            
        node_capacity = node_capacities[node_name]
        
        # Calculate resource shares
        cpu_share = resources['cpu'] / node_capacity['cpu']
        memory_share = resources['memory'] / node_capacity['memory']
        
        # The dominant share is the maximum of the resource shares
        dominant_share = max(cpu_share, memory_share)
        
        dominant_shares.append(dominant_share)
        cpu_shares.append(cpu_share)
        memory_shares.append(memory_share)
    
    # Calculate standard metrics
    if dominant_shares:
        fairness_metrics = {
            'avg_dominant_share': np.mean(dominant_shares),
            'min_dominant_share': np.min(dominant_shares),
            'max_dominant_share': np.max(dominant_shares),
            'stddev_dominant_share': np.std(dominant_shares),
            'avg_cpu_share': np.mean(cpu_shares),
            'avg_memory_share': np.mean(memory_shares),
            'cpu_memory_ratio': np.mean(cpu_shares) / np.mean(memory_shares) if np.mean(memory_shares) > 0 else 0
        }
    else:
        fairness_metrics = {
            'avg_dominant_share': 0,
            'min_dominant_share': 0,
            'max_dominant_share': 0,
            'stddev_dominant_share': 0,
            'avg_cpu_share': 0,
            'avg_memory_share': 0,
            'cpu_memory_ratio': 0
        }
    
    return fairness_metrics, dominant_shares, cpu_shares, memory_shares

def compare_drf_for_test_case(test_case):
    """Compare DRF metrics between default and scheduler with extender for a test case"""
    node_capacities = get_node_capacities()
    
    default_ns = f"sched-test{'-' + test_case if test_case else ''}"
    extender_ns = f"sched-test{'-' + test_case if test_case else ''}-extender"
    
    # Get pod placements
    default_placements = get_pod_placements(default_ns)
    extender_placements = get_pod_placements(extender_ns)
    
    # Calculate DRF metrics
    default_metrics, default_dom_shares, default_cpu, default_mem = calculate_drf_metrics(node_capacities, default_placements)
    extender_metrics, extender_dom_shares, extender_cpu, extender_mem = calculate_drf_metrics(node_capacities, extender_placements)
    
    # Save metrics to file
    with open(f'results/drf_analysis/test-case-{test_case}-metrics.json', 'w') as f:
        json.dump({
            'default_scheduler': default_metrics,
            'scheduler_with_extender': extender_metrics
        }, f, indent=2)
    
    # Create visualizations
    
    # 1. Dominant Share Distribution
    plt.figure(figsize=(12, 6))
    if default_dom_shares:
        plt.subplot(1, 2, 1)
        plt.hist(default_dom_shares, bins=10, alpha=0.7, label='Default Scheduler')
        plt.xlabel('Dominant Share')
        plt.ylabel('Number of Nodes')
        plt.title('Default Scheduler: Dominant Share Distribution')
        plt.grid(True, linestyle='--', alpha=0.7)
    
    if extender_dom_shares:
        plt.subplot(1, 2, 2)
        plt.hist(extender_dom_shares, bins=10, alpha=0.7, label='Scheduler with Extender', color='green')
        plt.xlabel('Dominant Share')
        plt.ylabel('Number of Nodes')
        plt.title('Scheduler with Extender: Dominant Share Distribution')
        plt.grid(True, linestyle='--', alpha=0.7)
    
    plt.tight_layout()
    plt.savefig(f'results/drf_analysis/test-case-{test_case}-dominant-share-dist.png')
    
    # 2. Resource Share Comparison
    plt.figure(figsize=(12, 6))
    
    # Data prep for bar chart
    metrics = ['avg_dominant_share', 'min_dominant_share', 'max_dominant_share', 'stddev_dominant_share']
    default_values = [default_metrics[m] for m in metrics]
    extender_values = [extender_metrics[m] for m in metrics]
    
    x = np.arange(len(metrics))
    width = 0.35
    
    plt.bar(x - width/2, default_values, width, label='Default Scheduler')
    plt.bar(x + width/2, extender_values, width, label='Scheduler with Extender', color='green')
    
    plt.xlabel('Metrics')
    plt.ylabel('Value')
    plt.title(f'Dominant Resource Fairness Metrics - Test Case {test_case}')
    plt.xticks(x, [m.replace('_', ' ').title() for m in metrics])
    plt.legend()
    plt.grid(True, linestyle='--', alpha=0.7)
    
    plt.tight_layout()
    plt.savefig(f'results/drf_analysis/test-case-{test_case}-drf-metrics.png')
    
    # 3. CPU vs Memory Share
    plt.figure(figsize=(10, 8))
    
    if default_cpu and default_mem:
        plt.subplot(2, 1, 1)
        plt.scatter(default_cpu, default_mem, alpha=0.7, label='Default Scheduler')
        plt.xlabel('CPU Share')
        plt.ylabel('Memory Share')
        plt.title('Default Scheduler: CPU vs Memory Share')
        # Add the perfect balance line
        max_share = max(max(default_cpu) if default_cpu else 0, max(default_mem) if default_mem else 0)
        plt.plot([0, max_share], [0, max_share], 'k--', alpha=0.5)
        plt.grid(True, linestyle='--', alpha=0.7)
        plt.legend()
    
    if extender_cpu and extender_mem:
        plt.subplot(2, 1, 2)
        plt.scatter(extender_cpu, extender_mem, alpha=0.7, label='Scheduler with Extender', color='green')
        plt.xlabel('CPU Share')
        plt.ylabel('Memory Share')
        plt.title('Scheduler with Extender: CPU vs Memory Share')
        # Add the perfect balance line
        max_share = max(max(extender_cpu) if extender_cpu else 0, max(extender_mem) if extender_mem else 0)
        plt.plot([0, max_share], [0, max_share], 'k--', alpha=0.5)
        plt.grid(True, linestyle='--', alpha=0.7)
        plt.legend()
    
    plt.tight_layout()
    plt.savefig(f'results/drf_analysis/test-case-{test_case}-cpu-vs-memory.png')
    
    return default_metrics, extender_metrics

def main():
    """Main function to run the DRF analysis"""
    print("Analyzing Dominant Resource Fairness for test cases...")
    
    test_cases = ["1", "2", "3"]
    comparison_data = {}
    
    for test_case in test_cases:
        print(f"Analyzing test case {test_case}...")
        default_metrics, extender_metrics = compare_drf_for_test_case(test_case)
        comparison_data[test_case] = {
            'default_scheduler': default_metrics,
            'scheduler_with_extender': extender_metrics
        }
    
    # Save overall comparison
    with open('results/drf_analysis/overall-comparison.json', 'w') as f:
        json.dump(comparison_data, f, indent=2)
    
    # Create summary visualization
    plt.figure(figsize=(12, 8))
    
    test_case_labels = [f"Test {tc}" for tc in test_cases]
    default_avg_dom_shares = [comparison_data[tc]['default_scheduler']['avg_dominant_share'] for tc in test_cases]
    extender_avg_dom_shares = [comparison_data[tc]['scheduler_with_extender']['avg_dominant_share'] for tc in test_cases]
    default_stddev_dom_shares = [comparison_data[tc]['default_scheduler']['stddev_dominant_share'] for tc in test_cases]
    extender_stddev_dom_shares = [comparison_data[tc]['scheduler_with_extender']['stddev_dominant_share'] for tc in test_cases]
    
    x = np.arange(len(test_cases))
    width = 0.35
    
    ax1 = plt.subplot(2, 1, 1)
    ax1.bar(x - width/2, default_avg_dom_shares, width, label='Default Scheduler')
    ax1.bar(x + width/2, extender_avg_dom_shares, width, label='Scheduler with Extender', color='green')
    
    ax1.set_xlabel('Test Case')
    ax1.set_ylabel('Average Dominant Share')
    ax1.set_title('Average Dominant Share by Test Case')
    ax1.set_xticks(x)
    ax1.set_xticklabels(test_case_labels)
    ax1.legend()
    ax1.grid(True, linestyle='--', alpha=0.7)
    
    ax2 = plt.subplot(2, 1, 2)
    ax2.bar(x - width/2, default_stddev_dom_shares, width, label='Default Scheduler')
    ax2.bar(x + width/2, extender_stddev_dom_shares, width, label='Scheduler with Extender', color='green')
    
    ax2.set_xlabel('Test Case')
    ax2.set_ylabel('Standard Deviation of Dominant Share')
    ax2.set_title('Fairness (Lower StdDev = More Fair) by Test Case')
    ax2.set_xticks(x)
    ax2.set_xticklabels(test_case_labels)
    ax2.legend()
    ax2.grid(True, linestyle='--', alpha=0.7)
    
    plt.tight_layout()
    plt.savefig('results/drf_analysis/overall-comparison.png')
    
    # Write a summary report
    with open('results/drf_analysis/summary.txt', 'w') as f:
        f.write("Dominant Resource Fairness Analysis Summary\n")
        f.write("=========================================\n\n")
        
        for tc in test_cases:
            f.write(f"Test Case {tc}:\n")
            f.write("-------------\n")
            
            def_metrics = comparison_data[tc]['default_scheduler']
            ext_metrics = comparison_data[tc]['scheduler_with_extender']
            
            f.write(f"Default Scheduler:\n")
            f.write(f"  - Average Dominant Share: {def_metrics['avg_dominant_share']:.4f}\n")
            f.write(f"  - Min/Max Dominant Share: {def_metrics['min_dominant_share']:.4f}/{def_metrics['max_dominant_share']:.4f}\n")
            f.write(f"  - Standard Deviation: {def_metrics['stddev_dominant_share']:.4f}\n")
            f.write(f"  - CPU/Memory Ratio: {def_metrics['cpu_memory_ratio']:.4f}\n\n")
            
            f.write(f"Scheduler with Extender:\n")
            f.write(f"  - Average Dominant Share: {ext_metrics['avg_dominant_share']:.4f}\n")
            f.write(f"  - Min/Max Dominant Share: {ext_metrics['min_dominant_share']:.4f}/{ext_metrics['max_dominant_share']:.4f}\n")
            f.write(f"  - Standard Deviation: {ext_metrics['stddev_dominant_share']:.4f}\n")
            f.write(f"  - CPU/Memory Ratio: {ext_metrics['cpu_memory_ratio']:.4f}\n\n")
            
            stddev_improvement = (def_metrics['stddev_dominant_share'] - ext_metrics['stddev_dominant_share']) / def_metrics['stddev_dominant_share'] * 100 if def_metrics['stddev_dominant_share'] > 0 else 0
            f.write(f"Fairness Improvement: {stddev_improvement:.2f}% (based on reduction in standard deviation)\n\n")
        
        f.write("\nOverall Conclusion:\n")
        f.write("------------------\n")
        
        # Calculate overall improvement across all test cases
        avg_stddev_default = np.mean([comparison_data[tc]['default_scheduler']['stddev_dominant_share'] for tc in test_cases])
        avg_stddev_extender = np.mean([comparison_data[tc]['scheduler_with_extender']['stddev_dominant_share'] for tc in test_cases])
        overall_improvement = (avg_stddev_default - avg_stddev_extender) / avg_stddev_default * 100 if avg_stddev_default > 0 else 0
        
        f.write(f"The scheduler with extender shows a {overall_improvement:.2f}% improvement in fairness ")
        f.write("based on the standard deviation of dominant shares across all test cases.\n\n")
        
        if overall_improvement > 10:
            f.write("The scheduler extender demonstrates a SIGNIFICANT improvement in enforcing dominant resource fairness.")
        elif overall_improvement > 0:
            f.write("The scheduler extender demonstrates a MODEST improvement in enforcing dominant resource fairness.")
        else:
            f.write("The scheduler extender does NOT show an improvement in enforcing dominant resource fairness compared to the default scheduler.")
    
    print("Analysis complete! Results saved to the results/drf_analysis/ directory.")

if __name__ == "__main__":
    main() 