// Package fakeprovisioner implements a dev/test-only backend controller that
// watches ProvisioningRequest objects and updates their status conditions
// deterministically. It is used by the Phase 7 local dev profile to exercise
// the Kueue ProvisioningRequest AdmissionCheck flow without a real
// cluster-autoscaler.
//
// Behavior is controlled via provisioningClassName conventions:
//
//	check-capacity.fake.dev  — delayed success (default 10s)
//	failed.fake.dev          — permanent failure
//	booking-expiry.fake.dev  — success then capacity revoked after expiry
//
// Tuning via ProvisioningRequest spec.parameters:
//
//	fake.dev/delay           — delay before success (default "10s")
//	fake.dev/expiry          — time after success before revocation (default "60s")
//	fake.dev/failure-message — custom failure message
package fakeprovisioner

import (
	"encoding/json"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GetConditions extracts status.conditions from an unstructured object,
// returning nil when no conditions are present.
func GetConditions(obj *unstructured.Unstructured) ([]metav1.Condition, error) {
	raw, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found || len(raw) == 0 {
		return nil, err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var conditions []metav1.Condition
	if err := json.Unmarshal(data, &conditions); err != nil {
		return nil, err
	}
	return conditions, nil
}

// SetConditions writes conditions to the unstructured object's status.
func SetConditions(obj *unstructured.Unstructured, conditions []metav1.Condition) error {
	data, err := json.Marshal(conditions)
	if err != nil {
		return err
	}
	var raw []interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if obj.Object["status"] == nil {
		obj.Object["status"] = map[string]interface{}{}
	}
	return unstructured.SetNestedSlice(obj.Object, raw, "status", "conditions")
}

// HasConditionTrue returns true when the named condition type has status True.
func HasConditionTrue(conditions []metav1.Condition, condType string) bool {
	for i := range conditions {
		if conditions[i].Type == condType && conditions[i].Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// FindCondition returns the first condition matching condType, or nil.
func FindCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// SetCondition updates an existing condition by type or appends a new one.
func SetCondition(conditions *[]metav1.Condition, cond metav1.Condition) {
	for i := range *conditions {
		if (*conditions)[i].Type == cond.Type {
			(*conditions)[i] = cond
			return
		}
	}
	*conditions = append(*conditions, cond)
}

// GetProvisioningClassName reads spec.provisioningClassName from an
// unstructured ProvisioningRequest.
func GetProvisioningClassName(obj *unstructured.Unstructured) string {
	val, _, _ := unstructured.NestedString(obj.Object, "spec", "provisioningClassName")
	return val
}

// GetParameters reads spec.parameters as map[string]string.
func GetParameters(obj *unstructured.Unstructured) map[string]string {
	raw, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "parameters")
	if !found {
		return nil
	}
	return raw
}

// GetParamDuration reads a duration parameter, returning defaultVal on
// missing or unparseable values.
func GetParamDuration(params map[string]string, key string, defaultVal time.Duration) time.Duration {
	if params == nil {
		return defaultVal
	}
	s, ok := params[key]
	if !ok || s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// GetParamString reads a string parameter, returning defaultVal on missing.
func GetParamString(params map[string]string, key string, defaultVal string) string {
	if params == nil {
		return defaultVal
	}
	s, ok := params[key]
	if !ok || s == "" {
		return defaultVal
	}
	return s
}
