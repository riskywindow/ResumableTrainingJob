package provisioning

import (
	"testing"

	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

func TestClassifyProvisioningNotConfiguredWhenNoACNames(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "some-check", State: kueuev1beta2.CheckStateReady},
	}
	got := ClassifyProvisioningFromChecks(checks, nil)
	if got != ProvisioningNotConfigured {
		t.Fatalf("expected NotConfigured for nil AC names, got %s", got)
	}

	got = ClassifyProvisioningFromChecks(checks, map[string]bool{})
	if got != ProvisioningNotConfigured {
		t.Fatalf("expected NotConfigured for empty AC names, got %s", got)
	}
}

func TestClassifyProvisioningNotConfiguredWhenNoMatch(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "resume-readiness", State: kueuev1beta2.CheckStateReady},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := ClassifyProvisioningFromChecks(checks, provACs)
	if got != ProvisioningNotConfigured {
		t.Fatalf("expected NotConfigured when no matching AC, got %s", got)
	}
}

func TestClassifyProvisioningPending(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "provision-ac", State: kueuev1beta2.CheckStatePending},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := ClassifyProvisioningFromChecks(checks, provACs)
	if got != ProvisioningPending {
		t.Fatalf("expected Pending, got %s", got)
	}
}

func TestClassifyProvisioningProvisioned(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "provision-ac", State: kueuev1beta2.CheckStateReady},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := ClassifyProvisioningFromChecks(checks, provACs)
	if got != ProvisioningProvisioned {
		t.Fatalf("expected Provisioned, got %s", got)
	}
}

func TestClassifyProvisioningFailed(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "provision-ac", State: kueuev1beta2.CheckStateRejected},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := ClassifyProvisioningFromChecks(checks, provACs)
	if got != ProvisioningFailed {
		t.Fatalf("expected Failed, got %s", got)
	}
}

func TestClassifyProvisioningRetry(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "provision-ac", State: kueuev1beta2.CheckStateRetry},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := ClassifyProvisioningFromChecks(checks, provACs)
	if got != ProvisioningRetry {
		t.Fatalf("expected Retry, got %s", got)
	}
}

func TestClassifyProvisioningUnknownStateDefaultsToPending(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "provision-ac", State: kueuev1beta2.CheckState("UnknownValue")},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := ClassifyProvisioningFromChecks(checks, provACs)
	if got != ProvisioningPending {
		t.Fatalf("expected Pending for unknown state, got %s", got)
	}
}

func TestClassifyProvisioningMultipleACsUsesFirstMatch(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "resume-readiness", State: kueuev1beta2.CheckStateReady},
		{Name: "provision-ac", State: kueuev1beta2.CheckStatePending},
		{Name: "other-ac", State: kueuev1beta2.CheckStateReady},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := ClassifyProvisioningFromChecks(checks, provACs)
	if got != ProvisioningPending {
		t.Fatalf("expected Pending, got %s", got)
	}
}

func TestFindProvisioningCheckNameFound(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "resume-readiness"},
		{Name: "provision-ac"},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := FindProvisioningCheckName(checks, provACs)
	if got != "provision-ac" {
		t.Fatalf("expected 'provision-ac', got %q", got)
	}
}

func TestFindProvisioningCheckNameNotFound(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "resume-readiness"},
	}
	provACs := map[string]bool{"provision-ac": true}
	got := FindProvisioningCheckName(checks, provACs)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestFindProvisioningCheckNameNilACNames(t *testing.T) {
	checks := []kueuev1beta2.AdmissionCheckState{
		{Name: "provision-ac"},
	}
	got := FindProvisioningCheckName(checks, nil)
	if got != "" {
		t.Fatalf("expected empty string for nil AC names, got %q", got)
	}
}

func TestResolveProvisioningRequestRefBasic(t *testing.T) {
	ref := ResolveProvisioningRequestRef("my-workload", "default", "provision-ac", 1)
	if ref == nil {
		t.Fatal("expected non-nil ref")
	}
	if ref.Name != "my-workload-provision-ac-1" {
		t.Fatalf("expected 'my-workload-provision-ac-1', got %q", ref.Name)
	}
	if ref.Namespace != "default" {
		t.Fatalf("expected namespace 'default', got %q", ref.Namespace)
	}
}

func TestResolveProvisioningRequestRefAttempt2(t *testing.T) {
	ref := ResolveProvisioningRequestRef("wl", "ns", "prov", 2)
	if ref == nil {
		t.Fatal("expected non-nil ref")
	}
	if ref.Name != "wl-prov-2" {
		t.Fatalf("expected 'wl-prov-2', got %q", ref.Name)
	}
}

func TestResolveProvisioningRequestRefDefaultsAttemptToOne(t *testing.T) {
	ref := ResolveProvisioningRequestRef("wl", "ns", "prov", 0)
	if ref == nil {
		t.Fatal("expected non-nil ref")
	}
	if ref.Name != "wl-prov-1" {
		t.Fatalf("expected 'wl-prov-1', got %q", ref.Name)
	}
}

func TestResolveProvisioningRequestRefNilForEmptyWorkload(t *testing.T) {
	ref := ResolveProvisioningRequestRef("", "ns", "prov", 1)
	if ref != nil {
		t.Fatal("expected nil ref for empty workload name")
	}
}

func TestResolveProvisioningRequestRefNilForEmptyCheck(t *testing.T) {
	ref := ResolveProvisioningRequestRef("wl", "ns", "", 1)
	if ref != nil {
		t.Fatal("expected nil ref for empty check name")
	}
}
