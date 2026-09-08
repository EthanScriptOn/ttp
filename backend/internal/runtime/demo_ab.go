package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	abExperimentLabel = "cicd.yuebuy.com/ab-experiment"
	abVariantLabel    = "cicd.yuebuy.com/ab-variant"
	abTrafficLabel    = "cicd.yuebuy.com/ab-traffic"
)

func (p *DemoProvider) DeployABExperiment(ctx context.Context, deployment ABExperimentDeployment) (err error) {
	log := deployment.Log
	defer func() {
		if err != nil {
			releaseLog(log, "k8s", "stderr", "ERROR", err.Error())
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	deployment.ClusterID = strings.TrimSpace(deployment.ClusterID)
	deployment.Namespace = strings.TrimSpace(deployment.Namespace)
	deployment.ProjectID = strings.TrimSpace(deployment.ProjectID)
	deployment.ExperimentID = strings.TrimSpace(deployment.ExperimentID)
	deployment.ACommitSHA = strings.TrimSpace(deployment.ACommitSHA)
	deployment.BCommitSHA = strings.TrimSpace(deployment.BCommitSHA)
	if deployment.ClusterID == "" || deployment.Namespace == "" || deployment.ProjectID == "" || deployment.ExperimentID == "" || deployment.ACommitSHA == "" || deployment.BCommitSHA == "" {
		return fmt.Errorf("%w: A/B 实验运行参数不完整", ErrInvalidRuntimeInput)
	}
	if deployment.Replicas <= 0 || deployment.Replicas > 100 {
		return fmt.Errorf("%w: replicas must be between 1 and 100", ErrInvalidRuntimeInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireCluster(deployment.ClusterID); err != nil {
		return err
	}
	releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("ab experiment=%s cluster=%s namespace=%s", deployment.ExperimentID, deployment.ClusterID, deployment.Namespace))
	if strings.EqualFold(strings.TrimSpace(deployment.Assignment), "user_id") || strings.EqualFold(strings.TrimSpace(deployment.Assignment), "json_field") {
		releaseLog(log, "gateway", "stdout", "INFO", fmt.Sprintf("routing source=%s path=%s algorithm=%s missing=%s", deployment.RoutingRule.Source, deployment.RoutingRule.Path, deployment.RoutingRule.Algorithm, deployment.RoutingRule.MissingBehavior))
	}
	for _, variant := range []struct {
		name, branch, commit, release, image string
		traffic                              int
	}{
		{name: "a", branch: deployment.ABranch, commit: deployment.ACommitSHA, release: deployment.AReleaseID, image: deployment.AImage, traffic: deployment.ATraffic},
		{name: "b", branch: deployment.BBranch, commit: deployment.BCommitSHA, release: deployment.BReleaseID, image: deployment.BImage, traffic: deployment.BTraffic},
	} {
		for ordinal := 1; ordinal <= deployment.Replicas; ordinal++ {
			name := demoABPodName(deployment.ProjectID, deployment.ExperimentID, variant.name, variant.commit, ordinal)
			for suffix := ordinal; ; suffix++ {
				if _, exists := p.pods[podKey(PodRef{ClusterID: deployment.ClusterID, Namespace: deployment.Namespace, Name: name})]; !exists {
					break
				}
				name = demoABPodName(deployment.ProjectID, deployment.ExperimentID, variant.name, variant.commit, suffix+1)
			}
			now := p.now().UTC()
			nodePrefix := "demo-node-"
			if demoMetricVariant(deployment.ClusterID) == "uat" {
				nodePrefix = "uat-node-"
			}
			detail := PodDetail{Pod: Pod{PodRef: PodRef{ClusterID: deployment.ClusterID, Namespace: deployment.Namespace, Name: name}, ProjectID: deployment.ProjectID, NodeName: fmt.Sprintf("%s%c", nodePrefix, 'a'+rune((ordinal-1)%3)), PodIP: fmt.Sprintf("10.2.%d.%d", ordinal, 20+ordinal), Phase: PodRunning, Ready: true, Labels: map[string]string{}, StartedAt: now}, Containers: map[string]ContainerStatus{"app": {Name: "app", Image: variant.image, Ready: true}}, Config: map[string]string{}, Environment: map[string]string{}}
			detail.Pod.Labels[ProjectLabelKey] = deployment.ProjectID
			detail.Pod.Labels["app"] = deployment.ProjectID
			detail.Pod.Labels[abExperimentLabel] = deployment.ExperimentID
			detail.Pod.Labels[abVariantLabel] = variant.name
			detail.Pod.Labels[abTrafficLabel] = fmt.Sprint(variant.traffic)
			detail.Pod.Labels["version"] = shortDemoSHA(variant.commit)
			detail.Pod.Labels["branch"] = variant.branch
			detail.Environment["CICD_AB_EXPERIMENT"] = deployment.ExperimentID
			detail.Environment["CICD_AB_VARIANT"] = variant.name
			detail.Environment["CICD_AB_TRAFFIC_PERCENT"] = fmt.Sprint(variant.traffic)
			detail.Environment["CICD_RELEASE_ID"] = variant.release
			detail.Environment["CICD_COMMIT_SHA"] = variant.commit
			detail.Environment["CICD_BRANCH"] = variant.branch
			for key, value := range detail.Environment {
				detail.Config[key] = value
			}
			p.pods[podKey(detail.PodRef)] = demoPod{detail: detail, logs: map[string]string{"app": fmt.Sprintf("%s A/B variant=%s release=%s commit=%s is running\n", now.Format(time.RFC3339), variant.name, variant.release, shortDemoSHA(variant.commit))}}
			releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("pod/%s variant=%s phase=Running ready=true traffic=%d%%", name, variant.name, variant.traffic))
		}
	}
	releaseLog(log, "k8s", "stdout", "INFO", fmt.Sprintf("A/B experiment %s ready A=%d pods B=%d pods", deployment.ExperimentID, deployment.Replicas, deployment.Replicas))
	return nil
}

func (p *DemoProvider) ListABExperimentPods(ctx context.Context, clusterID, namespace, projectID, experimentID string) ([]Pod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireCluster(clusterID); err != nil {
		return nil, err
	}
	items := make([]Pod, 0)
	for _, item := range p.pods {
		labels := item.detail.Labels
		if item.detail.ClusterID != clusterID || item.detail.Namespace != namespace || item.detail.ProjectID != projectID || labels[abExperimentLabel] != experimentID {
			continue
		}
		items = append(items, clonePod(item.detail.Pod))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Labels[abVariantLabel] == items[j].Labels[abVariantLabel] {
			return items[i].Name < items[j].Name
		}
		return items[i].Labels[abVariantLabel] < items[j].Labels[abVariantLabel]
	})
	return items, nil
}

func (p *DemoProvider) GetABExperimentMetrics(ctx context.Context, clusterID, namespace, projectID, experimentID string) (ABExperimentMetrics, error) {
	if err := ctx.Err(); err != nil {
		return ABExperimentMetrics{}, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireCluster(clusterID); err != nil {
		return ABExperimentMetrics{}, err
	}
	counts := map[string]int{"a": 0, "b": 0}
	traffic := map[string]int{"a": 0, "b": 0}
	for _, item := range p.pods {
		if item.detail.ClusterID != clusterID || item.detail.Namespace != namespace || item.detail.ProjectID != projectID || item.detail.Labels[abExperimentLabel] != experimentID {
			continue
		}
		variant := item.detail.Labels[abVariantLabel]
		if _, ok := counts[variant]; !ok {
			continue
		}
		counts[variant]++
		if traffic[variant] == 0 {
			var parsed int
			if _, scanErr := fmt.Sscanf(item.detail.Labels[abTrafficLabel], "%d", &parsed); scanErr == nil {
				traffic[variant] = parsed
			}
		}
	}
	metrics := func(variant string) ABVariantMetrics {
		if counts[variant] == 0 {
			return ABVariantMetrics{MetricsMessage: "当前分组暂无运行中的 Pod"}
		}
		// The demo runtime exposes a deterministic per-variant series so the
		// API and UI can be exercised before a Prometheus adapter is configured.
		requestRate := float64(traffic[variant])*2.4 + float64(counts[variant])*0.6
		errorRate := 0.6
		latency := 112.0
		if variant == "b" {
			errorRate = 0.9
			latency = 126
		}
		return ABVariantMetrics{MetricsAvailable: true, RequestRateRPS: requestRate, ErrorRatePercent: errorRate, LatencyP95MS: latency}
	}
	return ABExperimentMetrics{A: metrics("a"), B: metrics("b")}, nil
}

func (p *DemoProvider) UpdateABExperimentTraffic(ctx context.Context, clusterID, namespace, projectID, experimentID string, aTraffic, bTraffic int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if aTraffic < 0 || bTraffic < 0 || aTraffic > 100 || bTraffic > 100 || aTraffic+bTraffic != 100 {
		return fmt.Errorf("%w: A/B traffic must total 100", ErrInvalidRuntimeInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireCluster(clusterID); err != nil {
		return err
	}
	for key, item := range p.pods {
		if item.detail.ClusterID != clusterID || item.detail.Namespace != namespace || item.detail.ProjectID != projectID || item.detail.Labels[abExperimentLabel] != experimentID {
			continue
		}
		variant := item.detail.Labels[abVariantLabel]
		traffic := aTraffic
		if variant == "b" {
			traffic = bTraffic
		}
		item.detail.Labels[abTrafficLabel] = fmt.Sprint(traffic)
		item.detail.Environment["CICD_AB_TRAFFIC_PERCENT"] = fmt.Sprint(traffic)
		item.detail.Config["CICD_AB_TRAFFIC_PERCENT"] = fmt.Sprint(traffic)
		p.pods[key] = item
	}
	return nil
}

func (p *DemoProvider) StopABExperiment(ctx context.Context, clusterID, namespace, projectID, experimentID, keepVariant string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	keepVariant = strings.ToLower(strings.TrimSpace(keepVariant))
	if keepVariant != "" && keepVariant != "a" && keepVariant != "b" {
		return fmt.Errorf("%w: keep variant must be a or b", ErrInvalidRuntimeInput)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireCluster(clusterID); err != nil {
		return err
	}
	for key, item := range p.pods {
		if item.detail.ClusterID != clusterID || item.detail.Namespace != namespace || item.detail.ProjectID != projectID || item.detail.Labels[abExperimentLabel] != experimentID {
			continue
		}
		if keepVariant == "" || item.detail.Labels[abVariantLabel] != keepVariant {
			delete(p.pods, key)
			continue
		}
		item.detail.Labels[abTrafficLabel] = "100"
		item.detail.Environment["CICD_AB_TRAFFIC_PERCENT"] = "100"
		item.detail.Config["CICD_AB_TRAFFIC_PERCENT"] = "100"
		p.pods[key] = item
	}
	return nil
}

func (p *DemoProvider) CleanupABExperiment(ctx context.Context, clusterID, namespace, projectID, experimentID string) error {
	return p.StopABExperiment(ctx, clusterID, namespace, projectID, experimentID, "")
}

func demoABPodName(projectID, experimentID, variant, sha string, ordinal int) string {
	return fmt.Sprintf("%s-ab-%s-%s-%s-%d", sanitizeDemoToken(projectID), sanitizeDemoToken(experimentID), variant, sanitizeDemoToken(shortDemoSHA(sha)), ordinal)
}
