package store

import (
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

func TestNormalizeNamespaceQuotaRejectsDefaultsAboveRequestBudget(t *testing.T) {
	quota := DefaultNamespaceQuota()
	quota.DefaultCPURequest = "3"

	_, err := NormalizeNamespaceQuota(&quota)
	if err == nil || !strings.Contains(err.Error(), "default_cpu_request") {
		t.Fatalf("expected default request budget validation error, got %v", err)
	}
}

func TestNormalizeNamespaceQuotaFillsBoundedDefaults(t *testing.T) {
	normalized, err := NormalizeNamespaceQuota(&domain.NamespaceQuota{})
	if err != nil {
		t.Fatal(err)
	}
	if normalized != DefaultNamespaceQuota() {
		t.Fatalf("normalized empty quota = %#v, want %#v", normalized, DefaultNamespaceQuota())
	}
}
