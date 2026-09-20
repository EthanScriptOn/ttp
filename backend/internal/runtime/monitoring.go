package runtime

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	prometheusName          = "prometheus"
	prometheusNamespace     = "ttp-monitoring"
	prometheusVersion       = "v2.54.1"
	nodeExporterName        = "node-exporter"
	nodeExporterVersion     = "v1.8.2"
	kubeStateMetricsName    = "kube-state-metrics"
	kubeStateMetricsVersion = "v2.13.0"
)

var (
	appsDeploymentGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	appsDaemonSetGVR  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
)

func (p *KubernetesProvider) CheckMonitoring(ctx context.Context, clusterID string) (MonitoringStatus, error) {
	if _, err := p.clientFor(clusterID); err != nil {
		return MonitoringStatus{}, err
	}
	dynamicClient, err := p.dynamicClientFor(clusterID)
	if err != nil {
		return MonitoringStatus{}, fmt.Errorf("create dynamic kubernetes client: %w", err)
	}

	status := MonitoringStatus{
		Component:      prometheusName,
		DisplayName:    "Prometheus",
		Installable:    true,
		InstallVersion: prometheusVersion,
	}
	dependencies := []MonitoringDependency{
		monitoringDeploymentDependency(ctx, dynamicClient, prometheusName, "Prometheus", prometheusVersion),
		monitoringDaemonSetDependency(ctx, dynamicClient, nodeExporterName, "node-exporter", nodeExporterVersion),
		monitoringDeploymentDependency(ctx, dynamicClient, kubeStateMetricsName, "kube-state-metrics", kubeStateMetricsVersion),
	}
	status.Dependencies = dependencies
	status.Available = true
	status.Installed = true
	for _, dependency := range dependencies {
		status.Available = status.Available && dependency.Available
		status.Installed = status.Installed && dependency.Installed
	}
	status.HistoryAvailable = dependencies[0].Available

	switch {
	case status.Available:
		status.Message = "Prometheus、node-exporter 和 kube-state-metrics 已接入"
	case !dependencies[0].Installed:
		status.Message = "未检测到 Prometheus，可由平台安装"
	case !dependencies[0].Available:
		status.Message = "Prometheus 已安装，但尚未就绪"
	default:
		missing := make([]string, 0, len(dependencies))
		for _, dependency := range dependencies {
			if !dependency.Available {
				missing = append(missing, dependency.DisplayName)
			}
		}
		status.Message = fmt.Sprintf("监控依赖尚未就绪：%s", strings.Join(missing, "、"))
	}
	return status, nil
}

func monitoringDeploymentDependency(ctx context.Context, client dynamic.Interface, name, displayName, version string) MonitoringDependency {
	dependency := MonitoringDependency{
		Component:      name,
		DisplayName:    displayName,
		Installable:    true,
		InstallVersion: version,
		Message:        "未检测到，可由平台安装",
	}
	deployment, err := client.Resource(appsDeploymentGVR).Namespace(prometheusNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return dependency
	}
	if err != nil {
		dependency.Message = "检测失败"
		return dependency
	}
	dependency.Installed = true
	available, _, _ := unstructured.NestedInt64(deployment.Object, "status", "availableReplicas")
	if available > 0 {
		dependency.Available = true
		dependency.Message = "已接入"
	} else {
		dependency.Message = "已安装，但尚未就绪"
	}
	return dependency
}

func monitoringDaemonSetDependency(ctx context.Context, client dynamic.Interface, name, displayName, version string) MonitoringDependency {
	dependency := MonitoringDependency{
		Component:      name,
		DisplayName:    displayName,
		Installable:    true,
		InstallVersion: version,
		Message:        "未检测到，可由平台安装",
	}
	daemonSet, err := client.Resource(appsDaemonSetGVR).Namespace(prometheusNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return dependency
	}
	if err != nil {
		dependency.Message = "检测失败"
		return dependency
	}
	dependency.Installed = true
	desired, _, _ := unstructured.NestedInt64(daemonSet.Object, "status", "desiredNumberScheduled")
	ready, _, _ := unstructured.NestedInt64(daemonSet.Object, "status", "numberReady")
	if desired > 0 && ready >= desired {
		dependency.Available = true
		dependency.Message = "已接入"
	} else {
		dependency.Message = "已安装，但尚未就绪"
	}
	return dependency
}

func (p *KubernetesProvider) InstallMonitoring(ctx context.Context, clusterID string) (MonitoringStatus, error) {
	return p.InstallMonitoringWithRetention(ctx, clusterID, 7)
}

func (p *KubernetesProvider) InstallMonitoringWithRetention(ctx context.Context, clusterID string, retentionDays int) (MonitoringStatus, error) {
	if retentionDays != 3 && retentionDays != 7 && retentionDays != 14 && retentionDays != 30 {
		return MonitoringStatus{}, fmt.Errorf("monitoring retention must be 3, 7, 14, or 30 days")
	}
	if _, err := p.clientFor(clusterID); err != nil {
		return MonitoringStatus{}, err
	}
	dynamicClient, err := p.dynamicClientFor(clusterID)
	if err != nil {
		return MonitoringStatus{}, fmt.Errorf("create dynamic kubernetes client: %w", err)
	}
	for _, resource := range prometheusResources(retentionDays) {
		if err := applyUnstructured(ctx, dynamicClient, resource); err != nil {
			return MonitoringStatus{}, fmt.Errorf("apply %s/%s: %w", resource.GetKind(), resource.GetName(), err)
		}
	}
	return MonitoringStatus{
		Component:      prometheusName,
		DisplayName:    "Prometheus",
		Installable:    true,
		Installed:      true,
		InstallVersion: prometheusVersion,
		Message:        "Prometheus 监控组件已提交安装，等待采集链路就绪",
		RetentionDays:  retentionDays,
		Dependencies: []MonitoringDependency{
			{Component: prometheusName, DisplayName: "Prometheus", Installable: true, Installed: true, InstallVersion: prometheusVersion},
			{Component: nodeExporterName, DisplayName: "node-exporter", Installable: true, Installed: true, InstallVersion: nodeExporterVersion},
			{Component: kubeStateMetricsName, DisplayName: "kube-state-metrics", Installable: true, Installed: true, InstallVersion: kubeStateMetricsVersion},
		},
	}, nil
}

func (p *KubernetesProvider) dynamicClientFor(clusterID string) (dynamic.Interface, error) {
	p.mu.RLock()
	config := p.configs[clusterID]
	p.mu.RUnlock()
	if config == nil {
		return nil, fmt.Errorf("cluster %q has no rest config", clusterID)
	}
	return dynamic.NewForConfig(config)
}

func applyUnstructured(ctx context.Context, client dynamic.Interface, object *unstructured.Unstructured) error {
	resource := resourceForKind(object.GetKind())
	if resource.Resource == "" {
		return fmt.Errorf("unsupported resource kind %q", object.GetKind())
	}
	var target dynamic.ResourceInterface = client.Resource(resource)
	if object.GetNamespace() != "" {
		target = client.Resource(resource).Namespace(object.GetNamespace())
	}
	current, err := target.Get(ctx, object.GetName(), metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err = target.Create(ctx, object, metav1.CreateOptions{})
		return err
	}
	if object.GetKind() == "PersistentVolumeClaim" {
		return nil
	}
	if object.GetKind() == "Service" {
		for _, field := range []string{"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy", "healthCheckNodePort"} {
			if value, found, copyErr := unstructured.NestedFieldCopy(current.Object, "spec", field); copyErr == nil && found {
				if err := unstructured.SetNestedField(object.Object, value, "spec", field); err != nil {
					return err
				}
			}
		}
	}
	object.SetResourceVersion(current.GetResourceVersion())
	_, err = target.Update(ctx, object, metav1.UpdateOptions{})
	return err
}

func resourceForKind(kind string) schema.GroupVersionResource {
	switch kind {
	case "ServiceAccount":
		return schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}
	case "Service":
		return schema.GroupVersionResource{Version: "v1", Resource: "services"}
	case "ConfigMap":
		return schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	case "PersistentVolumeClaim":
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	case "ClusterRole", "ClusterRoleBinding":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: strings.ToLower(kind) + "s"}
	case "RoleBinding":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}
	case "Deployment":
		return appsDeploymentGVR
	case "DaemonSet":
		return appsDaemonSetGVR
	case "Namespace":
		return schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	default:
		return schema.GroupVersionResource{}
	}
}

func prometheusResources(retentionDays int) []*unstructured.Unstructured {
	const managedBy = "ttp"
	namespaceLabels := map[string]interface{}{"app.kubernetes.io/managed-by": managedBy}
	prometheusLabels := map[string]interface{}{"app.kubernetes.io/name": prometheusName, "app.kubernetes.io/managed-by": managedBy}
	nodeExporterLabels := map[string]interface{}{"app.kubernetes.io/name": nodeExporterName, "app.kubernetes.io/managed-by": managedBy}
	kubeStateMetricsLabels := map[string]interface{}{"app.kubernetes.io/name": kubeStateMetricsName, "app.kubernetes.io/managed-by": managedBy}
	prometheusConfig := `global:
  scrape_interval: 15s
scrape_configs:
- job_name: prometheus
  static_configs:
  - targets: ['localhost:9090']
- job_name: kubernetes-nodes-cadvisor
  scheme: https
  metrics_path: /metrics/cadvisor
  bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
  tls_config:
    insecure_skip_verify: true
  kubernetes_sd_configs:
  - role: node
  relabel_configs:
  - action: replace
    source_labels: [__meta_kubernetes_node_name]
    target_label: __metrics_path__
    replacement: /api/v1/nodes/$1/proxy/metrics/cadvisor
  - action: replace
    target_label: __address__
    replacement: kubernetes.default.svc:443
- job_name: node-exporter
  kubernetes_sd_configs:
  - role: pod
    namespaces:
      names: [ttp-monitoring]
  relabel_configs:
  - action: keep
    source_labels: [__meta_kubernetes_pod_label_app_kubernetes_io_name]
    regex: node-exporter
  - action: replace
    source_labels: [__meta_kubernetes_pod_ip]
    target_label: __address__
    replacement: $1:9100
  - action: replace
    source_labels: [__meta_kubernetes_pod_node_name]
    target_label: kubernetes_node
- job_name: kubernetes-pods
  kubernetes_sd_configs:
  - role: pod
  relabel_configs:
  - action: keep
    source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
    regex: "true"
  - action: replace
    source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
    target_label: __metrics_path__
    regex: (.+)
  - action: replace
    source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scheme]
    target_label: __scheme__
    regex: (https?)
  - action: replace
    source_labels: [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
    target_label: __address__
    regex: ([^:]+)(?::\d+)?;(\d+)
    replacement: $1:$2
  - action: labelmap
    regex: __meta_kubernetes_pod_label_(.+)
  - action: replace
    source_labels: [__meta_kubernetes_namespace]
    target_label: kubernetes_namespace
  - action: replace
    source_labels: [__meta_kubernetes_pod_name]
    target_label: kubernetes_pod_name
- job_name: kube-state-metrics
  static_configs:
  - targets: ['kube-state-metrics.ttp-monitoring.svc:8080']
`

	return []*unstructured.Unstructured{
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "Namespace",
			"metadata": map[string]interface{}{"name": prometheusNamespace, "labels": namespaceLabels},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "ServiceAccount",
			"metadata": map[string]interface{}{"name": prometheusName, "namespace": prometheusNamespace, "labels": prometheusLabels},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "ServiceAccount",
			"metadata": map[string]interface{}{"name": nodeExporterName, "namespace": prometheusNamespace, "labels": nodeExporterLabels},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "ServiceAccount",
			"metadata": map[string]interface{}{"name": kubeStateMetricsName, "namespace": prometheusNamespace, "labels": kubeStateMetricsLabels},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]interface{}{"name": prometheusName, "namespace": prometheusNamespace, "labels": prometheusLabels},
			"data":     map[string]interface{}{"prometheus.yml": prometheusConfig},
		}),
		object(map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole",
			"metadata": map[string]interface{}{"name": "ttp-prometheus", "labels": prometheusLabels},
			"rules": []interface{}{
				map[string]interface{}{"apiGroups": []interface{}{""}, "resources": []interface{}{"nodes", "nodes/proxy", "namespaces", "services", "endpoints", "pods"}, "verbs": []interface{}{"get", "list", "watch"}},
			},
		}),
		object(map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding",
			"metadata": map[string]interface{}{"name": "ttp-prometheus", "labels": prometheusLabels},
			"roleRef":  map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "ttp-prometheus"},
			"subjects": []interface{}{map[string]interface{}{"kind": "ServiceAccount", "name": prometheusName, "namespace": prometheusNamespace}},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "PersistentVolumeClaim",
			"metadata": map[string]interface{}{"name": prometheusName, "namespace": prometheusNamespace, "labels": prometheusLabels},
			"spec":     map[string]interface{}{"accessModes": []interface{}{"ReadWriteOnce"}, "resources": map[string]interface{}{"requests": map[string]interface{}{"storage": "5Gi"}}},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "Service",
			"metadata": map[string]interface{}{"name": prometheusName, "namespace": prometheusNamespace, "labels": prometheusLabels},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{"app.kubernetes.io/name": prometheusName},
				"ports":    []interface{}{map[string]interface{}{"name": "http", "port": int64(9090), "targetPort": int64(9090)}},
			},
		}),
		object(map[string]interface{}{
			"apiVersion": "apps/v1", "kind": "Deployment",
			"metadata": map[string]interface{}{"name": prometheusName, "namespace": prometheusNamespace, "labels": prometheusLabels},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app.kubernetes.io/name": prometheusName}},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{"labels": prometheusLabels},
					"spec": map[string]interface{}{
						"serviceAccountName": prometheusName,
						"containers": []interface{}{map[string]interface{}{
							"name":           prometheusName,
							"image":          "prom/prometheus:" + prometheusVersion,
							"args":           []interface{}{"--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.path=/prometheus", "--storage.tsdb.retention.time=" + fmt.Sprintf("%dd", retentionDays), "--web.enable-lifecycle"},
							"ports":          []interface{}{map[string]interface{}{"name": "http", "containerPort": int64(9090)}},
							"readinessProbe": map[string]interface{}{"httpGet": map[string]interface{}{"path": "/-/ready", "port": int64(9090)}, "initialDelaySeconds": int64(5), "periodSeconds": int64(10)},
							"livenessProbe":  map[string]interface{}{"httpGet": map[string]interface{}{"path": "/-/healthy", "port": int64(9090)}, "initialDelaySeconds": int64(30), "periodSeconds": int64(20)},
							"resources":      map[string]interface{}{"requests": map[string]interface{}{"cpu": "100m", "memory": "256Mi"}, "limits": map[string]interface{}{"cpu": "1", "memory": "1Gi"}},
							"volumeMounts":   []interface{}{map[string]interface{}{"name": "config", "mountPath": "/etc/prometheus"}, map[string]interface{}{"name": "data", "mountPath": "/prometheus"}},
						}},
						"volumes": []interface{}{map[string]interface{}{"name": "config", "configMap": map[string]interface{}{"name": prometheusName}}, map[string]interface{}{"name": "data", "persistentVolumeClaim": map[string]interface{}{"claimName": prometheusName}}},
					},
				},
			},
		}),
		object(map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole",
			"metadata": map[string]interface{}{"name": "ttp-kube-state-metrics", "labels": kubeStateMetricsLabels},
			"rules": []interface{}{
				map[string]interface{}{"apiGroups": []interface{}{""}, "resources": []interface{}{"configmaps", "endpoints", "limitranges", "namespaces", "nodes", "persistentvolumeclaims", "pods", "replicationcontrollers", "resourcequotas", "secrets", "services", "serviceaccounts", "persistentvolumes"}, "verbs": []interface{}{"get", "list", "watch"}},
				map[string]interface{}{"apiGroups": []interface{}{"apps"}, "resources": []interface{}{"daemonsets", "deployments", "replicasets", "statefulsets"}, "verbs": []interface{}{"get", "list", "watch"}},
				map[string]interface{}{"apiGroups": []interface{}{"batch"}, "resources": []interface{}{"cronjobs", "jobs"}, "verbs": []interface{}{"get", "list", "watch"}},
				map[string]interface{}{"apiGroups": []interface{}{"autoscaling"}, "resources": []interface{}{"horizontalpodautoscalers"}, "verbs": []interface{}{"get", "list", "watch"}},
				map[string]interface{}{"apiGroups": []interface{}{"discovery.k8s.io"}, "resources": []interface{}{"endpointslices"}, "verbs": []interface{}{"get", "list", "watch"}},
				map[string]interface{}{"apiGroups": []interface{}{"networking.k8s.io"}, "resources": []interface{}{"ingresses"}, "verbs": []interface{}{"get", "list", "watch"}},
				map[string]interface{}{"apiGroups": []interface{}{"policy"}, "resources": []interface{}{"poddisruptionbudgets"}, "verbs": []interface{}{"get", "list", "watch"}},
				map[string]interface{}{"apiGroups": []interface{}{"storage.k8s.io"}, "resources": []interface{}{"storageclasses", "volumeattachments"}, "verbs": []interface{}{"get", "list", "watch"}},
			},
		}),
		object(map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding",
			"metadata": map[string]interface{}{"name": "ttp-kube-state-metrics", "labels": kubeStateMetricsLabels},
			"roleRef":  map[string]interface{}{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "ttp-kube-state-metrics"},
			"subjects": []interface{}{map[string]interface{}{"kind": "ServiceAccount", "name": kubeStateMetricsName, "namespace": prometheusNamespace}},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "Service",
			"metadata": map[string]interface{}{"name": kubeStateMetricsName, "namespace": prometheusNamespace, "labels": kubeStateMetricsLabels},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{"app.kubernetes.io/name": kubeStateMetricsName},
				"ports":    []interface{}{map[string]interface{}{"name": "http", "port": int64(8080), "targetPort": int64(8080)}},
			},
		}),
		object(map[string]interface{}{
			"apiVersion": "apps/v1", "kind": "Deployment",
			"metadata": map[string]interface{}{"name": kubeStateMetricsName, "namespace": prometheusNamespace, "labels": kubeStateMetricsLabels},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app.kubernetes.io/name": kubeStateMetricsName}},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{"labels": kubeStateMetricsLabels},
					"spec": map[string]interface{}{
						"serviceAccountName": kubeStateMetricsName,
						"containers": []interface{}{map[string]interface{}{
							"name":           kubeStateMetricsName,
							"image":          "registry.k8s.io/kube-state-metrics/kube-state-metrics:" + kubeStateMetricsVersion,
							"args":           []interface{}{"--port=8080"},
							"ports":          []interface{}{map[string]interface{}{"name": "http", "containerPort": int64(8080)}},
							"readinessProbe": map[string]interface{}{"httpGet": map[string]interface{}{"path": "/healthz", "port": int64(8080)}, "initialDelaySeconds": int64(5), "periodSeconds": int64(10)},
							"resources":      map[string]interface{}{"requests": map[string]interface{}{"cpu": "50m", "memory": "64Mi"}, "limits": map[string]interface{}{"cpu": "200m", "memory": "256Mi"}},
						}},
					},
				},
			},
		}),
		object(map[string]interface{}{
			"apiVersion": "v1", "kind": "Service",
			"metadata": map[string]interface{}{"name": nodeExporterName, "namespace": prometheusNamespace, "labels": nodeExporterLabels},
			"spec": map[string]interface{}{
				"clusterIP": "None",
				"selector":  map[string]interface{}{"app.kubernetes.io/name": nodeExporterName},
				"ports":     []interface{}{map[string]interface{}{"name": "metrics", "port": int64(9100), "targetPort": int64(9100)}},
			},
		}),
		object(map[string]interface{}{
			"apiVersion": "apps/v1", "kind": "DaemonSet",
			"metadata": map[string]interface{}{"name": nodeExporterName, "namespace": prometheusNamespace, "labels": nodeExporterLabels},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app.kubernetes.io/name": nodeExporterName}},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{"labels": nodeExporterLabels},
					"spec": map[string]interface{}{
						"serviceAccountName": nodeExporterName,
						"hostNetwork":        true,
						"hostPID":            true,
						"tolerations":        []interface{}{map[string]interface{}{"operator": "Exists"}},
						"containers": []interface{}{map[string]interface{}{
							"name":            nodeExporterName,
							"image":           "quay.io/prometheus/node-exporter:" + nodeExporterVersion,
							"args":            []interface{}{"--path.rootfs=/host/root", "--path.procfs=/host/proc", "--path.sysfs=/host/sys"},
							"ports":           []interface{}{map[string]interface{}{"name": "metrics", "containerPort": int64(9100), "hostPort": int64(9100)}},
							"securityContext": map[string]interface{}{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "runAsNonRoot": true, "runAsUser": int64(65534), "capabilities": map[string]interface{}{"drop": []interface{}{"ALL"}}},
							"resources":       map[string]interface{}{"requests": map[string]interface{}{"cpu": "50m", "memory": "64Mi"}, "limits": map[string]interface{}{"cpu": "200m", "memory": "256Mi"}},
							"volumeMounts":    []interface{}{map[string]interface{}{"name": "root", "mountPath": "/host/root", "readOnly": true}, map[string]interface{}{"name": "proc", "mountPath": "/host/proc", "readOnly": true}, map[string]interface{}{"name": "sys", "mountPath": "/host/sys", "readOnly": true}},
						}},
						"volumes": []interface{}{map[string]interface{}{"name": "root", "hostPath": map[string]interface{}{"path": "/", "type": "Directory"}}, map[string]interface{}{"name": "proc", "hostPath": map[string]interface{}{"path": "/proc", "type": "Directory"}}, map[string]interface{}{"name": "sys", "hostPath": map[string]interface{}{"path": "/sys", "type": "Directory"}}},
					},
				},
			},
		}),
	}
}

func object(value map[string]interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: value}
}
