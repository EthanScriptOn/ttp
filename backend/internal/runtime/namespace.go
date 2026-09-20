package runtime

const (
	NamespaceManagedByLabel   = "app.kubernetes.io/managed-by"
	NamespaceManagedByValue   = "ttp"
	NamespaceSpaceIDLabel     = "cicd.yuebuy.com/space-id"
	NamespaceEnvironmentLabel = "cicd.yuebuy.com/environment"
)

// NamespaceLabels identifies the TTP owner of a generated environment
// namespace. The labels are deliberately namespace-scoped metadata: projects
// in one TTP space/environment may share the namespace, while another space
// cannot accidentally take it over.
func NamespaceLabels(spaceID, environment string) map[string]string {
	return map[string]string{
		NamespaceManagedByLabel:   NamespaceManagedByValue,
		NamespaceSpaceIDLabel:     spaceID,
		NamespaceEnvironmentLabel: environment,
	}
}
