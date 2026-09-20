package runtime

import (
	"context"
	"fmt"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultRuntimeServiceAccountNamespace = "ttp-system"
	defaultRuntimeServiceAccountName      = "ttp-runtime"
	namespaceAccessClusterRoleName        = "ttp-runtime-namespace-access"
	namespaceAccessRoleBindingName        = "ttp-runtime"
)

// reconcileNamespaceRuntimeAccess binds the TTP runtime ServiceAccount to the
// preinstalled namespace access ClusterRole inside one generated namespace.
// The ClusterRole remains centrally managed by the deployment template; TTP
// owns only the per-namespace RoleBinding.
func (p *KubernetesProvider) reconcileNamespaceRuntimeAccess(ctx context.Context, client kubernetes.Interface, namespace string, labels map[string]string) error {
	subject, err := p.runtimeServiceAccountSubject()
	if err != nil {
		return err
	}
	desired := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespaceAccessRoleBindingName,
			Namespace: namespace,
			Labels:    cloneStringMap(labels),
		},
		Subjects: []rbacv1.Subject{subject},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     namespaceAccessClusterRoleName,
		},
	}

	bindings := client.RbacV1().RoleBindings(namespace)
	existing, err := bindings.Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, createErr := bindings.Create(ctx, desired, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(createErr) {
			existing, err = bindings.Get(ctx, desired.Name, metav1.GetOptions{})
		} else {
			return createErr
		}
	}
	if err != nil {
		return err
	}
	if !namespaceLabelsMatch(existing.Labels, labels) {
		return fmt.Errorf("%w: RoleBinding %s/%s", ErrNamespaceOwnershipConflict, namespace, desired.Name)
	}
	if !sameRoleRef(existing.RoleRef, desired.RoleRef) {
		if deleteErr := bindings.Delete(ctx, desired.Name, metav1.DeleteOptions{}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return deleteErr
		}
		_, createErr := bindings.Create(ctx, desired, metav1.CreateOptions{})
		if createErr == nil || apierrors.IsAlreadyExists(createErr) {
			return nil
		}
		return createErr
	}
	desired.ResourceVersion = existing.ResourceVersion
	if _, err := bindings.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil
}

func (p *KubernetesProvider) runtimeServiceAccountSubject() (rbacv1.Subject, error) {
	namespace := strings.TrimSpace(p.runtimeServiceAccountNamespace)
	name := strings.TrimSpace(p.runtimeServiceAccountName)
	if err := validateNamespace(namespace); err != nil {
		return rbacv1.Subject{}, fmt.Errorf("%w: runtime ServiceAccount namespace is invalid", err)
	}
	if name == "" || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return rbacv1.Subject{}, fmt.Errorf("%w: runtime ServiceAccount name is invalid", ErrInvalidKubernetesInput)
	}
	return rbacv1.Subject{Kind: rbacv1.ServiceAccountKind, Namespace: namespace, Name: name}, nil
}

func sameRoleRef(left, right rbacv1.RoleRef) bool {
	return left.APIGroup == right.APIGroup && left.Kind == right.Kind && left.Name == right.Name
}
