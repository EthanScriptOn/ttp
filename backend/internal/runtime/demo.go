package runtime

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type demoPod struct {
	detail PodDetail
	logs   map[string]string
}

// DemoProvider is a local, mutable runtime fixture. It deliberately has no
// Kubernetes client and cannot affect a real cluster.
type DemoProvider struct {
	mu       sync.RWMutex
	now      func() time.Time
	pods     map[string]demoPod
	clusters map[string]struct{}
}

func NewDemoProvider() *DemoProvider {
	started := time.Date(2026, time.January, 12, 8, 30, 0, 0, time.UTC)
	provider := &DemoProvider{now: func() time.Time { return time.Now().UTC() }, pods: make(map[string]demoPod), clusters: map[string]struct{}{"demo-cluster": {}, "demo-cluster-uat": {}}}
	provider.pods[podKey(PodRef{ClusterID: "demo-cluster", Namespace: "lab", Name: "reverse-lab-api-7d9f8c6d4b-x2k9m"})] = demoPod{
		detail: PodDetail{Pod: Pod{PodRef: PodRef{ClusterID: "demo-cluster", Namespace: "lab", Name: "reverse-lab-api-7d9f8c6d4b-x2k9m"}, ProjectID: "reverse-lab", NodeName: "demo-node-a", PodIP: "10.0.0.21", Phase: PodRunning, Ready: true, RestartCount: 0, Labels: map[string]string{"app": "reverse-lab-api", "version": "v1"}, StartedAt: started}, Containers: map[string]ContainerStatus{"api": {Name: "api", Image: "example.invalid/reverse-lab:v1", Ready: true}}, Config: map[string]string{"LOG_LEVEL": "info", "FEATURE_FLAGS": "runtime"}, Environment: map[string]string{"API_PORT": "8080", "RUNTIME_MODE": "demo"}},
		logs:   map[string]string{"api": "2026-01-12T08:30:03Z server listening on :8080\n2026-01-12T08:30:04Z runtime provider ready\n"},
	}
	provider.pods[podKey(PodRef{ClusterID: "demo-cluster", Namespace: "lab", Name: "worker-5f7b9d7f6b-q8p4r"})] = demoPod{
		detail: PodDetail{Pod: Pod{PodRef: PodRef{ClusterID: "demo-cluster", Namespace: "lab", Name: "worker-5f7b9d7f6b-q8p4r"}, ProjectID: "reverse-lab", NodeName: "demo-node-b", PodIP: "10.0.0.22", Phase: PodRunning, Ready: true, RestartCount: 1, Labels: map[string]string{"app": "worker", "version": "v1"}, StartedAt: started.Add(-2 * time.Hour)}, Containers: map[string]ContainerStatus{"worker": {Name: "worker", Image: "example.invalid/reverse-lab-worker:v1", Ready: true, RestartCount: 1}}, Config: map[string]string{"QUEUE": "runtime-jobs"}, Environment: map[string]string{"CONCURRENCY": "4"}},
		logs:   map[string]string{"worker": "2026-01-12T08:31:00Z worker started\n"},
	}
	provider.pods[podKey(PodRef{ClusterID: "demo-cluster-uat", Namespace: "uat", Name: "reverse-lab-api-uat-6f8d7c5b4a-p3m7n"})] = demoPod{
		detail: PodDetail{Pod: Pod{PodRef: PodRef{ClusterID: "demo-cluster-uat", Namespace: "uat", Name: "reverse-lab-api-uat-6f8d7c5b4a-p3m7n"}, ProjectID: "reverse-lab", NodeName: "uat-node-a", PodIP: "10.1.0.21", Phase: PodRunning, Ready: true, RestartCount: 0, Labels: map[string]string{"app": "reverse-lab-api", "version": "v1"}, StartedAt: started.Add(-24 * time.Hour)}, Containers: map[string]ContainerStatus{"api": {Name: "api", Image: "example.invalid/reverse-lab:v1", Ready: true}}, Config: map[string]string{"LOG_LEVEL": "info", "FEATURE_FLAGS": "runtime"}, Environment: map[string]string{"API_PORT": "8080", "RUNTIME_MODE": "uat"}},
		logs:   map[string]string{"api": "2026-01-11T08:30:03Z server listening on :8080\n2026-01-11T08:30:04Z uat runtime provider ready\n"},
	}
	provider.pods[podKey(PodRef{ClusterID: "demo-cluster-uat", Namespace: "uat", Name: "worker-uat-5b6c7d8e9f-r4s8t"})] = demoPod{
		detail: PodDetail{Pod: Pod{PodRef: PodRef{ClusterID: "demo-cluster-uat", Namespace: "uat", Name: "worker-uat-5b6c7d8e9f-r4s8t"}, ProjectID: "reverse-lab", NodeName: "uat-node-b", PodIP: "10.1.0.22", Phase: PodRunning, Ready: true, RestartCount: 0, Labels: map[string]string{"app": "worker", "version": "v1"}, StartedAt: started.Add(-24 * time.Hour)}, Containers: map[string]ContainerStatus{"worker": {Name: "worker", Image: "example.invalid/reverse-lab-worker:v1", Ready: true}}, Config: map[string]string{"QUEUE": "runtime-jobs"}, Environment: map[string]string{"CONCURRENCY": "4", "RUNTIME_MODE": "uat"}},
		logs:   map[string]string{"worker": "2026-01-11T08:31:00Z uat worker started\n"},
	}
	return provider
}

func (p *DemoProvider) ListPods(ctx context.Context, clusterID, projectID string) ([]Pod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireCluster(clusterID); err != nil {
		return nil, err
	}
	result := make([]Pod, 0)
	for _, item := range p.pods {
		if item.detail.ClusterID != clusterID || (projectID != "" && item.detail.ProjectID != projectID) {
			continue
		}
		result = append(result, clonePod(item.detail.Pod))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (p *DemoProvider) RegisterKubeconfigWithContext(clusterID, _, _ string) error {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return ErrInvalidRuntimeInput
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clusters[clusterID] = struct{}{}
	return nil
}

func (p *DemoProvider) RegisterInCluster(clusterID string) error {
	return p.RegisterKubeconfigWithContext(clusterID, "", "")
}

func (p *DemoProvider) CheckCluster(ctx context.Context, clusterID string) (ClusterConnection, error) {
	if err := ctx.Err(); err != nil {
		return ClusterConnection{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireCluster(strings.TrimSpace(clusterID)); err != nil {
		return ClusterConnection{}, err
	}
	return ClusterConnection{Version: "demo"}, nil
}

func (p *DemoProvider) ListPodsInNamespace(ctx context.Context, clusterID, namespace, projectID string) ([]Pod, error) {
	items, err := p.ListPods(ctx, clusterID, projectID)
	if err != nil {
		return nil, err
	}
	if namespace == "" {
		return items, nil
	}
	filtered := make([]Pod, 0, len(items))
	for _, item := range items {
		if item.Namespace == namespace {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (p *DemoProvider) GetPod(ctx context.Context, ref PodRef) (PodDetail, error) {
	if err := ctx.Err(); err != nil {
		return PodDetail{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	item, err := p.lookup(ref)
	if err != nil {
		return PodDetail{}, err
	}
	return cloneDetail(item.detail), nil
}

func (p *DemoProvider) GetPodLogs(ctx context.Context, request PodLogRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	item, err := p.lookup(request.PodRef)
	if err != nil {
		return "", err
	}
	if request.Container == "" && len(item.logs) == 1 {
		for name := range item.logs {
			request.Container = name
		}
	}
	logs, ok := item.logs[request.Container]
	if !ok {
		return "", ErrContainerNotFound
	}
	if request.TailLines <= 0 {
		return logs, nil
	}
	lines := strings.Split(strings.TrimSuffix(logs, "\n"), "\n")
	if request.TailLines >= len(lines) {
		return logs, nil
	}
	return strings.Join(lines[len(lines)-request.TailLines:], "\n") + "\n", nil
}

func (p *DemoProvider) UpdatePodConfig(ctx context.Context, ref PodRef, update PodConfigUpdate) (PodDetail, error) {
	if err := ctx.Err(); err != nil {
		return PodDetail{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := podKey(ref)
	item, ok := p.pods[key]
	if !ok {
		return PodDetail{}, ErrPodNotFound
	}
	if item.detail.Config == nil {
		item.detail.Config = make(map[string]string)
	}
	if item.detail.Environment == nil {
		item.detail.Environment = make(map[string]string)
	}
	for name, value := range update.Config {
		item.detail.Config[name] = value
	}
	for name, value := range update.Environment {
		item.detail.Environment[name] = value
	}
	p.pods[key] = item
	return cloneDetail(item.detail), nil
}

// DeployRelease updates the in-memory workload so a local release can be
// followed all the way from a selected commit to healthy Pods. It is only a
// demo provider operation; no Kubernetes API or external registry is touched.
func (p *DemoProvider) DeployRelease(ctx context.Context, deployment ReleaseDeployment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	deployment.ClusterID = strings.TrimSpace(deployment.ClusterID)
	deployment.Namespace = strings.TrimSpace(deployment.Namespace)
	deployment.ProjectID = strings.TrimSpace(deployment.ProjectID)
	deployment.ReleaseID = strings.TrimSpace(deployment.ReleaseID)
	deployment.CommitSHA = strings.TrimSpace(deployment.CommitSHA)
	deployment.Image = strings.TrimSpace(deployment.Image)
	if deployment.ClusterID == "" || deployment.Namespace == "" || deployment.ProjectID == "" || deployment.ReleaseID == "" || deployment.CommitSHA == "" || deployment.Image == "" {
		return fmt.Errorf("%w: release deployment fields are required", ErrInvalidRuntimeInput)
	}
	if deployment.Replicas <= 0 {
		deployment.Replicas = 1
	}
	if deployment.Replicas > 100 {
		return fmt.Errorf("%w: replicas must be between 1 and 100", ErrInvalidRuntimeInput)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireCluster(deployment.ClusterID); err != nil {
		return err
	}

	keys := make([]string, 0)
	for key, item := range p.pods {
		if item.detail.ClusterID == deployment.ClusterID && item.detail.Namespace == deployment.Namespace && item.detail.ProjectID == deployment.ProjectID {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return p.pods[keys[i]].detail.Name < p.pods[keys[j]].detail.Name })
	for len(keys) > deployment.Replicas {
		delete(p.pods, keys[len(keys)-1])
		keys = keys[:len(keys)-1]
	}

	now := p.now().UTC()
	nodePrefix := "demo-node-"
	if demoMetricVariant(deployment.ClusterID) == "uat" {
		nodePrefix = "uat-node-"
	}
	for _, key := range keys {
		item := p.pods[key]
		item.detail = applyDemoRelease(item.detail, deployment, now)
		p.pods[key] = item
	}

	for index := len(keys); index < deployment.Replicas; index++ {
		name := demoReleasePodName(deployment.ProjectID, deployment.CommitSHA, index+1)
		for {
			if _, exists := p.pods[podKey(PodRef{ClusterID: deployment.ClusterID, Namespace: deployment.Namespace, Name: name})]; !exists {
				break
			}
			name = demoReleasePodName(deployment.ProjectID, deployment.CommitSHA, index+2)
		}
		containerName := "app"
		detail := PodDetail{Pod: Pod{PodRef: PodRef{ClusterID: deployment.ClusterID, Namespace: deployment.Namespace, Name: name}, ProjectID: deployment.ProjectID, NodeName: fmt.Sprintf("%s%c", nodePrefix, 'a'+rune(index%3)), PodIP: fmt.Sprintf("10.0.0.%d", 30+index), Phase: PodRunning, Ready: true, Labels: map[string]string{}, StartedAt: now}, Containers: map[string]ContainerStatus{containerName: {Name: containerName, Image: deployment.Image, Ready: true}}, Config: map[string]string{}, Environment: map[string]string{}}
		detail = applyDemoRelease(detail, deployment, now)
		p.pods[podKey(detail.PodRef)] = demoPod{detail: detail, logs: map[string]string{containerName: fmt.Sprintf("%s release %s is running\n", now.Format(time.RFC3339), shortDemoSHA(deployment.CommitSHA))}}
	}
	return nil
}

func applyDemoRelease(detail PodDetail, deployment ReleaseDeployment, now time.Time) PodDetail {
	detail = cloneDetail(detail)
	if detail.Pod.Labels == nil {
		detail.Pod.Labels = make(map[string]string)
	}
	detail.Pod.Labels[ProjectLabelKey] = deployment.ProjectID
	detail.Pod.Labels["app"] = deployment.ProjectID
	detail.Pod.Labels["release"] = deployment.ReleaseID
	detail.Pod.Labels["version"] = shortDemoSHA(deployment.CommitSHA)
	detail.Pod.Labels["branch"] = deployment.Branch
	detail.Pod.Phase = PodRunning
	detail.Pod.Ready = true
	detail.Pod.StartedAt = now
	if detail.Config == nil {
		detail.Config = make(map[string]string)
	}
	if detail.Environment == nil {
		detail.Environment = make(map[string]string)
	}
	detail.Environment["CICD_RELEASE_ID"] = deployment.ReleaseID
	detail.Environment["CICD_COMMIT_SHA"] = deployment.CommitSHA
	detail.Environment["CICD_BRANCH"] = deployment.Branch
	detail.Environment["CICD_STRATEGY"] = deployment.Strategy
	detail.Environment["CICD_STABLE_PERCENT"] = fmt.Sprint(deployment.StablePercent)
	detail.Environment["CICD_CANDIDATE_PERCENT"] = fmt.Sprint(deployment.CandidatePercent)
	detail.Environment["CICD_BLUE_PERCENT"] = fmt.Sprint(deployment.BluePercent)
	detail.Environment["CICD_GREEN_PERCENT"] = fmt.Sprint(deployment.GreenPercent)
	for key, value := range detail.Environment {
		if strings.HasPrefix(key, "CICD_") {
			detail.Config[key] = value
		}
	}
	for name, container := range detail.Containers {
		container.Image = deployment.Image
		container.Ready = true
		detail.Containers[name] = container
	}
	return detail
}

func demoReleasePodName(projectID, sha string, ordinal int) string {
	base := sanitizeDemoToken(projectID)
	version := sanitizeDemoToken(shortDemoSHA(sha))
	name := fmt.Sprintf("%s-%s-%d", base, version, ordinal)
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	if name == "" {
		name = fmt.Sprintf("app-%d", ordinal)
	}
	return name
}

func sanitizeDemoToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastDash = false
		} else if builder.Len() > 0 && !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func shortDemoSHA(value string) string {
	value = sanitizeDemoToken(value)
	if len(value) > 10 {
		return value[:10]
	}
	if value == "" {
		return "version"
	}
	return value
}

func (p *DemoProvider) GetClusterMetrics(ctx context.Context, clusterID string) (ClusterMetrics, error) {
	if err := ctx.Err(); err != nil {
		return ClusterMetrics{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireCluster(clusterID); err != nil {
		return ClusterMetrics{}, err
	}
	var total, healthy int
	for _, item := range p.pods {
		if item.detail.ClusterID == clusterID {
			total++
			if item.detail.Ready {
				healthy++
			}
		}
	}
	now := p.now().UTC()
	metrics := ClusterMetrics{
		ClusterID:           clusterID,
		CPUUsedPercent:      34.5,
		MemoryUsedPercent:   48.2,
		SwapUsedPercent:     8.3,
		DiskUsedPercent:     62.4,
		DiskReadMbps:        42.0,
		DiskWriteMbps:       18.0,
		NetworkReceiveMbps:  86.0,
		NetworkTransmitMbps: 52.0,
		Load1:               1.82,
		Load5:               1.55,
		Load15:              1.31,
		RequestRateRPS:      285.0,
		ErrorRatePercent:    0.8,
		LatencyP50Ms:        42.0,
		LatencyP95Ms:        118.0,
		LatencyP99Ms:        246.0,
		PodRestartCount:     1,
		PendingPodCount:     0,
		FailedPodCount:      0,
		CrashLoopCount:      0,
		OOMKilledCount:      0,
		NodeCount:           3,
		ReadyNodeCount:      3,
		DeploymentDesired:   total,
		DeploymentAvailable: healthy,
		MetricsSource:       "demo",
		MetricsAvailable:    true,
		PodCount:            total,
		HealthyPodCount:     healthy,
		ObservedAt:          now,
		Series:              demoMetricSeries(now, false, total, healthy),
		Nodes:               demoNodeMetrics(),
	}
	if demoMetricVariant(clusterID) == "uat" {
		metrics.CPUUsedPercent = 41.8
		metrics.MemoryUsedPercent = 55.4
		metrics.DiskUsedPercent = 67.8
		metrics.NetworkReceiveMbps = 103
		metrics.NetworkTransmitMbps = 64
		metrics.Load1 = 2.14
		metrics.Load5 = 1.74
		metrics.Load15 = 1.48
		metrics.RequestRateRPS = 318
		metrics.ErrorRatePercent = 1.1
		metrics.LatencyP50Ms = 48
		metrics.LatencyP95Ms = 132
		metrics.LatencyP99Ms = 278
		metrics.Series = demoMetricSeries(now, false, total, healthy, "uat")
		metrics.Nodes = demoNodeMetrics("uat")
	}
	return metrics, nil
}

func (p *DemoProvider) GetProjectMetrics(ctx context.Context, clusterID, projectID string) (ProjectMetrics, error) {
	if err := ctx.Err(); err != nil {
		return ProjectMetrics{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireCluster(clusterID); err != nil {
		return ProjectMetrics{}, err
	}
	var total, healthy int
	for _, item := range p.pods {
		if item.detail.ClusterID == clusterID && item.detail.ProjectID == projectID {
			total++
			if item.detail.Ready {
				healthy++
			}
		}
	}
	if total == 0 {
		return ProjectMetrics{}, ErrProjectNotFound
	}
	now := p.now().UTC()
	metrics := ProjectMetrics{
		ClusterID:           clusterID,
		ProjectID:           projectID,
		PodCount:            total,
		HealthyPodCount:     healthy,
		CPUUsedPercent:      18.7,
		MemoryUsedPercent:   27.1,
		SwapUsedPercent:     3.1,
		DiskUsedPercent:     46.8,
		DiskReadMbps:        21.0,
		DiskWriteMbps:       9.0,
		NetworkReceiveMbps:  32.0,
		NetworkTransmitMbps: 19.0,
		Load1:               0.74,
		Load5:               0.62,
		Load15:              0.58,
		RequestRateRPS:      96.0,
		ErrorRatePercent:    0.3,
		LatencyP50Ms:        28.0,
		LatencyP95Ms:        82.0,
		LatencyP99Ms:        161.0,
		PodRestartCount:     1,
		PendingPodCount:     0,
		FailedPodCount:      0,
		CrashLoopCount:      0,
		OOMKilledCount:      0,
		NodeCount:           2,
		ReadyNodeCount:      2,
		DeploymentDesired:   total,
		DeploymentAvailable: healthy,
		MetricsSource:       "demo",
		MetricsAvailable:    true,
		ObservedAt:          now,
		Series:              demoMetricSeries(now, true, total, healthy),
		Nodes:               demoProjectNodeMetrics(),
	}
	if demoMetricVariant(clusterID) == "uat" {
		metrics.CPUUsedPercent = 24.3
		metrics.MemoryUsedPercent = 39.6
		metrics.SwapUsedPercent = 4.2
		metrics.DiskUsedPercent = 51.4
		metrics.DiskReadMbps = 28
		metrics.DiskWriteMbps = 13
		metrics.NetworkReceiveMbps = 45
		metrics.NetworkTransmitMbps = 25
		metrics.Load1 = 0.92
		metrics.Load5 = 0.76
		metrics.Load15 = 0.68
		metrics.RequestRateRPS = 132
		metrics.ErrorRatePercent = 0.5
		metrics.LatencyP50Ms = 31
		metrics.LatencyP95Ms = 94
		metrics.LatencyP99Ms = 182
		metrics.Series = demoMetricSeries(now, true, total, healthy, "uat")
		metrics.Nodes = demoProjectNodeMetrics("uat")
	}
	return metrics, nil
}

func (p *DemoProvider) GetProjectMetricsInNamespace(ctx context.Context, clusterID, namespace, projectID string) (ProjectMetrics, error) {
	items, err := p.ListPodsInNamespace(ctx, clusterID, namespace, projectID)
	if err != nil {
		return ProjectMetrics{}, err
	}
	if len(items) == 0 {
		return ProjectMetrics{
			ClusterID:        clusterID,
			ProjectID:        projectID,
			MetricsAvailable: false,
			MetricsMessage:   "当前命名空间暂无运行中的 Pod",
			ObservedAt:       p.now().UTC(),
		}, nil
	}
	healthy := 0
	for _, item := range items {
		if item.Ready {
			healthy++
		}
	}
	metrics, err := p.GetProjectMetrics(ctx, clusterID, projectID)
	if err != nil {
		return ProjectMetrics{}, err
	}
	metrics.PodCount = len(items)
	metrics.HealthyPodCount = healthy
	return metrics, nil
}

func (p *DemoProvider) requireCluster(clusterID string) error {
	if _, ok := p.clusters[clusterID]; !ok {
		return ErrClusterNotFound
	}
	return nil
}

// demoMetricSeries gives the dashboard a useful, stable-looking time window
// without pretending it came from a real metrics backend. The final sample is
// aligned with the snapshot values above so cards and charts agree.
func demoMetricSeries(now time.Time, project bool, podCount, healthyPodCount int, variants ...string) []MetricPoint {
	const (
		points = 289
		step   = 5 * time.Minute
	)
	series := make([]MetricPoint, 0, points)
	for index := 0; index < points; index++ {
		age := float64(index - (points - 1))
		wave := func(amplitude, period float64) float64 {
			return amplitude * math.Sin(age/period)
		}
		if project {
			series = append(series, MetricPoint{
				Timestamp:           now.Add(time.Duration(index-(points-1)) * step),
				CPUUsedPercent:      clampMetric(18.7+wave(5.2, 8), 0, 100),
				MemoryUsedPercent:   clampMetric(27.1+wave(3.8, 13), 0, 100),
				SwapUsedPercent:     clampMetric(3.1+wave(1.2, 17), 0, 100),
				DiskUsedPercent:     clampMetric(46.8+wave(1.4, 29), 0, 100),
				DiskReadMbps:        clampMetric(21+wave(10, 7), 0, 1000),
				DiskWriteMbps:       clampMetric(9+wave(5, 9), 0, 1000),
				NetworkReceiveMbps:  clampMetric(32+wave(15, 6), 0, 10000),
				NetworkTransmitMbps: clampMetric(19+wave(9, 8), 0, 10000),
				Load1:               clampMetric(0.74+wave(0.24, 8), 0, 100),
				Load5:               clampMetric(0.62+wave(0.16, 11), 0, 100),
				Load15:              clampMetric(0.58+wave(0.11, 15), 0, 100),
				RequestRateRPS:      clampMetric(96+wave(28, 7), 0, 100000),
				ErrorRatePercent:    clampMetric(0.3+wave(0.12, 10), 0, 100),
				LatencyP50Ms:        clampMetric(28+wave(8, 8), 0, 60000),
				LatencyP95Ms:        clampMetric(82+wave(22, 9), 0, 60000),
				LatencyP99Ms:        clampMetric(161+wave(38, 12), 0, 60000),
				PodCount:            podCount,
				HealthyPodCount:     healthyPodCount,
				PodRestartCount:     1,
			})
			continue
		}
		series = append(series, MetricPoint{
			Timestamp:           now.Add(time.Duration(index-(points-1)) * step),
			CPUUsedPercent:      clampMetric(34.5+wave(8.5, 8), 0, 100),
			MemoryUsedPercent:   clampMetric(48.2+wave(6.2, 13), 0, 100),
			SwapUsedPercent:     clampMetric(8.3+wave(2.4, 17), 0, 100),
			DiskUsedPercent:     clampMetric(62.4+wave(1.8, 29), 0, 100),
			DiskReadMbps:        clampMetric(42+wave(20, 7), 0, 1000),
			DiskWriteMbps:       clampMetric(18+wave(11, 9), 0, 1000),
			NetworkReceiveMbps:  clampMetric(86+wave(36, 6), 0, 10000),
			NetworkTransmitMbps: clampMetric(52+wave(25, 8), 0, 10000),
			Load1:               clampMetric(1.82+wave(0.54, 8), 0, 100),
			Load5:               clampMetric(1.55+wave(0.36, 11), 0, 100),
			Load15:              clampMetric(1.31+wave(0.25, 15), 0, 100),
			RequestRateRPS:      clampMetric(285+wave(90, 7), 0, 100000),
			ErrorRatePercent:    clampMetric(0.8+wave(0.28, 10), 0, 100),
			LatencyP50Ms:        clampMetric(42+wave(12, 8), 0, 60000),
			LatencyP95Ms:        clampMetric(118+wave(34, 9), 0, 60000),
			LatencyP99Ms:        clampMetric(246+wave(62, 12), 0, 60000),
			PodCount:            podCount,
			HealthyPodCount:     healthyPodCount,
			PodRestartCount:     1,
		})
	}
	variant := ""
	if len(variants) > 0 {
		variant = strings.ToLower(strings.TrimSpace(variants[0]))
	}
	if variant == "uat" {
		for index := range series {
			if project {
				series[index].CPUUsedPercent += 5.6
				series[index].MemoryUsedPercent += 12.5
				series[index].SwapUsedPercent += 1.1
				series[index].DiskUsedPercent += 4.6
				series[index].DiskReadMbps += 7
				series[index].DiskWriteMbps += 4
				series[index].NetworkReceiveMbps += 13
				series[index].NetworkTransmitMbps += 6
				series[index].Load1 += 0.18
				series[index].Load5 += 0.14
				series[index].Load15 += 0.10
				series[index].RequestRateRPS += 36
				series[index].ErrorRatePercent += 0.2
				series[index].LatencyP50Ms += 3
				series[index].LatencyP95Ms += 12
				series[index].LatencyP99Ms += 21
			} else {
				series[index].CPUUsedPercent += 7.3
				series[index].MemoryUsedPercent += 7.2
				series[index].DiskUsedPercent += 5.4
				series[index].NetworkReceiveMbps += 17
				series[index].NetworkTransmitMbps += 12
				series[index].Load1 += 0.32
				series[index].Load5 += 0.19
				series[index].Load15 += 0.17
				series[index].RequestRateRPS += 33
				series[index].ErrorRatePercent += 0.3
				series[index].LatencyP50Ms += 6
				series[index].LatencyP95Ms += 14
				series[index].LatencyP99Ms += 32
			}
		}
	}
	return series
}

func demoNodeMetrics(variants ...string) []NodeMetrics {
	variant := ""
	if len(variants) > 0 {
		variant = strings.ToLower(strings.TrimSpace(variants[0]))
	}
	if variant == "uat" {
		return []NodeMetrics{
			{Name: "uat-node-a", Ready: true, CPUUsedPercent: 48.3, MemoryUsedPercent: 57.2, SwapUsedPercent: 7.1, DiskUsedPercent: 64.8, Load1: 1.62, NetworkReceiveMbps: 57.0, NetworkTransmitMbps: 34.0, PodCount: 1},
			{Name: "uat-node-b", Ready: true, CPUUsedPercent: 35.6, MemoryUsedPercent: 51.1, SwapUsedPercent: 4.5, DiskUsedPercent: 70.2, Load1: 1.24, NetworkReceiveMbps: 46.0, NetworkTransmitMbps: 30.0, PodCount: 1},
			{Name: "uat-node-c", Ready: true, CPUUsedPercent: 39.2, MemoryUsedPercent: 53.8, SwapUsedPercent: 5.2, DiskUsedPercent: 68.5, Load1: 1.41, NetworkReceiveMbps: 35.0, NetworkTransmitMbps: 22.0, PodCount: 0},
		}
	}
	return []NodeMetrics{
		{Name: "demo-node-a", Ready: true, CPUUsedPercent: 41.2, MemoryUsedPercent: 52.8, SwapUsedPercent: 6.4, DiskUsedPercent: 58.4, Load1: 1.36, NetworkReceiveMbps: 46.0, NetworkTransmitMbps: 29.0, PodCount: 1},
		{Name: "demo-node-b", Ready: true, CPUUsedPercent: 29.7, MemoryUsedPercent: 44.1, SwapUsedPercent: 3.1, DiskUsedPercent: 61.7, Load1: 1.02, NetworkReceiveMbps: 31.0, NetworkTransmitMbps: 18.0, PodCount: 1},
		{Name: "demo-node-c", Ready: true, CPUUsedPercent: 32.6, MemoryUsedPercent: 47.6, SwapUsedPercent: 4.8, DiskUsedPercent: 67.1, Load1: 1.18, NetworkReceiveMbps: 28.0, NetworkTransmitMbps: 16.0, PodCount: 0},
	}
}

func demoProjectNodeMetrics(variants ...string) []NodeMetrics {
	all := demoNodeMetrics(variants...)
	if len(all) > 2 {
		return all[:2]
	}
	return all
}

func demoMetricVariant(clusterID string) string {
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(clusterID)), "-uat") {
		return "uat"
	}
	return "default"
}

func clampMetric(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
func (p *DemoProvider) lookup(ref PodRef) (demoPod, error) {
	if err := p.requireCluster(ref.ClusterID); err != nil {
		return demoPod{}, err
	}
	item, ok := p.pods[podKey(ref)]
	if !ok {
		return demoPod{}, ErrPodNotFound
	}
	if ref.ProjectID != "" && item.detail.ProjectID != ref.ProjectID {
		return demoPod{}, ErrPodNotFound
	}
	return item, nil
}
func podKey(ref PodRef) string {
	return fmt.Sprintf("%s\x00%s\x00%s", ref.ClusterID, ref.Namespace, ref.Name)
}
func clonePod(pod Pod) Pod { pod.Labels = cloneMap(pod.Labels); return pod }
func cloneDetail(detail PodDetail) PodDetail {
	detail.Pod = clonePod(detail.Pod)
	if detail.Containers != nil {
		containers := make(map[string]ContainerStatus, len(detail.Containers))
		for name, container := range detail.Containers {
			containers[name] = container
		}
		detail.Containers = containers
	}
	detail.Config = cloneMap(detail.Config)
	detail.Environment = cloneMap(detail.Environment)
	return detail
}
func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
