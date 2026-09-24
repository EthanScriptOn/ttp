package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const (
	registryProbeTimeout  = 60 * time.Second
	registryProbeInterval = 500 * time.Millisecond
)

// TestRegistryPull creates a disposable Pod in an existing TTP-managed
// environment namespace. The Pod uses the same registry credential format as
// a release imagePullSecret, so the check exercises the actual kubelet/
// containerd pull path instead of only the control plane's HTTP client.
func (p *KubernetesProvider) TestRegistryPull(ctx context.Context, clusterID string, request RegistryPullTestRequest) (result RegistryPullTestResult, err error) {
	if err := validateClusterID(clusterID); err != nil {
		return result, err
	}
	if err := validateRegistryPullTestRequest(request); err != nil {
		return result, err
	}
	client, err := p.clientFor(clusterID)
	if err != nil {
		return result, err
	}
	probeContext, cancel := context.WithTimeout(ctx, registryProbeTimeout)
	defer cancel()
	requestContext, requestCancel := p.requestContext(probeContext)
	namespace, err := p.registryProbeNamespace(requestContext, client)
	requestCancel()
	if err != nil {
		return result, err
	}

	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	if len(suffix) > 12 {
		suffix = suffix[len(suffix)-12:]
	}
	secretName := "ttp-registry-probe-" + suffix
	podName := "ttp-registry-pull-" + suffix
	labelsForProbe := map[string]string{
		managedByLabelKey:                "ttp",
		"cicd.yuebuy.com/registry-probe": "true",
	}
	secret, err := p.imagePullSecretForDeployment(ReleaseDeployment{
		Namespace: namespace,
		ProjectID: "registry-probe",
		ReleaseID: "registry-probe",
		Image:     request.Image,
		ImagePullCredential: &ImagePullCredential{
			Registry:   request.Registry,
			AuthType:   request.AuthType,
			Username:   request.Username,
			Secret:     request.Secret,
			SecretName: request.SecretName,
		},
	})
	if err != nil {
		return result, err
	}
	secret.Name = secretName
	secret.Labels = labelsForProbe

	requestContext, requestCancel = p.requestContext(probeContext)
	_, err = client.CoreV1().Secrets(namespace).Create(requestContext, secret, metav1.CreateOptions{})
	requestCancel()
	if err != nil {
		return result, fmt.Errorf("创建 Registry 探测凭证失败：%w", err)
	}
	secretCreated := true
	podCreated := false
	cleanup := func() error {
		cleanupContext, cleanupCancel := p.requestContext(context.Background())
		defer cleanupCancel()
		var cleanupErr error
		if podCreated {
			if deleteErr := client.CoreV1().Pods(namespace).Delete(cleanupContext, podName, metav1.DeleteOptions{}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				cleanupErr = fmt.Errorf("删除 Registry 探测 Pod 失败：%w", deleteErr)
			}
		}
		if secretCreated {
			if deleteErr := client.CoreV1().Secrets(namespace).Delete(cleanupContext, secretName, metav1.DeleteOptions{}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) && cleanupErr == nil {
				cleanupErr = fmt.Errorf("删除 Registry 探测凭证失败：%w", deleteErr)
			}
		}
		return cleanupErr
	}
	defer func() {
		if cleanupErr := cleanup(); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
	}()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: namespace, Labels: labelsForProbe},
		Spec: corev1.PodSpec{
			RestartPolicy:    corev1.RestartPolicyNever,
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: secretName}},
			Containers:       []corev1.Container{{Name: "pull-probe", Image: request.Image, ImagePullPolicy: corev1.PullAlways}},
		},
	}
	requestContext, requestCancel = p.requestContext(probeContext)
	_, err = client.CoreV1().Pods(namespace).Create(requestContext, pod, metav1.CreateOptions{})
	requestCancel()
	if err != nil {
		return result, fmt.Errorf("创建 Registry 拉取探测 Pod 失败：%w", err)
	}
	podCreated = true

	result = RegistryPullTestResult{Namespace: namespace, PodName: podName, Image: request.Image}
	ticker := time.NewTicker(registryProbeInterval)
	defer ticker.Stop()
	for {
		requestContext, requestCancel = p.requestContext(probeContext)
		current, getErr := client.CoreV1().Pods(namespace).Get(requestContext, podName, metav1.GetOptions{})
		requestCancel()
		if getErr != nil {
			if apierrors.IsNotFound(getErr) {
				return result, fmt.Errorf("Registry 拉取探测 Pod 被意外删除")
			}
			return result, fmt.Errorf("读取 Registry 拉取探测 Pod 失败：%w", getErr)
		}
		result.NodeName = current.Spec.NodeName
		result.Phase = PodPhase(current.Status.Phase)
		if message := registryProbeImagePullFailure(current); message != "" {
			return result, fmt.Errorf("%s", message)
		}
		if registryProbeStarted(current) {
			result.Message = "K3s 节点已成功拉取测试镜像"
			return result, nil
		}
		if current.Status.Phase == corev1.PodFailed {
			return result, fmt.Errorf("Registry 拉取探测 Pod 失败：%s", podFailureMessage(current))
		}
		select {
		case <-probeContext.Done():
			if probeContext.Err() == context.DeadlineExceeded {
				return result, fmt.Errorf("Registry 拉取探测超时，Pod 尚未完成镜像拉取")
			}
			return result, probeContext.Err()
		case <-ticker.C:
		}
	}
}

func validateRegistryPullTestRequest(request RegistryPullTestRequest) error {
	image := strings.TrimSpace(request.Image)
	registry := strings.ToLower(strings.TrimSpace(request.Registry))
	if image == "" || registry == "" || request.Secret == "" || request.SecretName == "" {
		return fmt.Errorf("%w: Registry 拉取测试参数不完整", ErrInvalidKubernetesInput)
	}
	imageHost := strings.ToLower(strings.SplitN(image, "/", 2)[0])
	if imageHost != registry {
		return fmt.Errorf("%w: 测试镜像与镜像仓库连接不匹配", ErrInvalidKubernetesInput)
	}
	if request.AuthType != "basic" && request.AuthType != "token" {
		return fmt.Errorf("%w: Registry 鉴权类型无效", ErrInvalidKubernetesInput)
	}
	return nil
}

func (p *KubernetesProvider) registryProbeNamespace(ctx context.Context, client kubernetes.Interface) (string, error) {
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labels.Set{NamespaceManagedByLabel: NamespaceManagedByValue}.AsSelector().String()})
	if err != nil {
		return "", fmt.Errorf("查找 TTP 环境 namespace 失败：%w", err)
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("当前集群没有可用于镜像拉取测试的 TTP 环境 namespace")
	}
	return list.Items[0].Name, nil
}

func registryProbeStarted(pod *corev1.Pod) bool {
	for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		if status.State.Running != nil || status.State.Terminated != nil {
			return true
		}
	}
	return pod.Status.Phase == corev1.PodSucceeded
}

func registryProbeImagePullFailure(pod *corev1.Pod) string {
	for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		if status.State.Waiting == nil {
			continue
		}
		reason := strings.TrimSpace(status.State.Waiting.Reason)
		if reason != "ErrImagePull" && reason != "ImagePullBackOff" && reason != "InvalidImageName" && reason != "RegistryUnavailable" {
			continue
		}
		detail := strings.Join(strings.Fields(status.State.Waiting.Message), " ")
		if detail == "" {
			detail = reason
		}
		return fmt.Sprintf("K3s 节点镜像拉取失败（%s）：%s", reason, detail)
	}
	return ""
}

func podFailureMessage(pod *corev1.Pod) string {
	if message := strings.Join(strings.Fields(pod.Status.Message), " "); message != "" {
		return message
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated != nil && strings.TrimSpace(status.State.Terminated.Message) != "" {
			return strings.Join(strings.Fields(status.State.Terminated.Message), " ")
		}
	}
	return "Pod 未能启动"
}
