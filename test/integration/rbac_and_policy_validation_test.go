package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// ---------------------------------------------------------------------------
// ValidatingAdmissionPolicy structural tests
// ---------------------------------------------------------------------------

func TestPolicyManifestsExist(t *testing.T) {
	root := findProjectRoot(t)
	policyDir := filepath.Join(root, "deploy", "prod", "policies")

	requiredFiles := []string{
		"require-queue-assignment.yaml",
		"deny-direct-jobset.yaml",
		"deny-direct-workload.yaml",
	}

	for _, f := range requiredFiles {
		path := filepath.Join(policyDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("required policy file missing: %s", f)
		}
	}
}

// vapDocument represents the minimal structure of a ValidatingAdmissionPolicy
// or ValidatingAdmissionPolicyBinding YAML document.
type vapDocument struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec vapSpec `json:"spec"`
}

type vapSpec struct {
	// ValidatingAdmissionPolicy fields
	FailurePolicy    string            `json:"failurePolicy"`
	MatchConstraints *vapMatchConstraints `json:"matchConstraints"`
	MatchConditions  []vapMatchCondition  `json:"matchConditions"`
	Validations      []vapValidation      `json:"validations"`

	// ValidatingAdmissionPolicyBinding fields
	PolicyName        string   `json:"policyName"`
	ValidationActions []string `json:"validationActions"`
}

type vapMatchConstraints struct {
	ResourceRules []vapResourceRule `json:"resourceRules"`
}

type vapResourceRule struct {
	APIGroups   []string `json:"apiGroups"`
	APIVersions []string `json:"apiVersions"`
	Operations  []string `json:"operations"`
	Resources   []string `json:"resources"`
}

type vapMatchCondition struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

type vapValidation struct {
	Expression        string `json:"expression"`
	Message           string `json:"message"`
	MessageExpression string `json:"messageExpression"`
	Reason            string `json:"reason"`
}

func parsePolicyFile(t *testing.T, path string) []vapDocument {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}

	var docs []vapDocument
	for _, raw := range strings.Split(string(data), "---") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var doc vapDocument
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}
		if doc.Kind != "" {
			docs = append(docs, doc)
		}
	}
	return docs
}

func TestPolicyManifestsAreValidYAML(t *testing.T) {
	root := findProjectRoot(t)
	policyDir := filepath.Join(root, "deploy", "prod", "policies")

	files := []string{
		"require-queue-assignment.yaml",
		"deny-direct-jobset.yaml",
		"deny-direct-workload.yaml",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			docs := parsePolicyFile(t, filepath.Join(policyDir, f))
			if len(docs) < 2 {
				t.Fatalf("expected at least 2 documents (policy + binding), got %d", len(docs))
			}

			// First doc should be the policy
			policy := docs[0]
			if policy.Kind != "ValidatingAdmissionPolicy" {
				t.Errorf("first document kind = %q, want ValidatingAdmissionPolicy", policy.Kind)
			}
			if policy.APIVersion != "admissionregistration.k8s.io/v1" {
				t.Errorf("policy apiVersion = %q, want admissionregistration.k8s.io/v1", policy.APIVersion)
			}

			// Second doc should be the binding
			binding := docs[1]
			if binding.Kind != "ValidatingAdmissionPolicyBinding" {
				t.Errorf("second document kind = %q, want ValidatingAdmissionPolicyBinding", binding.Kind)
			}
			if binding.APIVersion != "admissionregistration.k8s.io/v1" {
				t.Errorf("binding apiVersion = %q, want admissionregistration.k8s.io/v1", binding.APIVersion)
			}
		})
	}
}

func TestPolicyBindingsReferenceCorrectPolicy(t *testing.T) {
	root := findProjectRoot(t)
	policyDir := filepath.Join(root, "deploy", "prod", "policies")

	files := []string{
		"require-queue-assignment.yaml",
		"deny-direct-jobset.yaml",
		"deny-direct-workload.yaml",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			docs := parsePolicyFile(t, filepath.Join(policyDir, f))

			var policyName string
			var bindingRef string

			for _, doc := range docs {
				switch doc.Kind {
				case "ValidatingAdmissionPolicy":
					policyName = doc.Metadata.Name
				case "ValidatingAdmissionPolicyBinding":
					bindingRef = doc.Spec.PolicyName
				}
			}

			if policyName == "" {
				t.Fatal("no ValidatingAdmissionPolicy found")
			}
			if bindingRef == "" {
				t.Fatal("no ValidatingAdmissionPolicyBinding found")
			}
			if policyName != bindingRef {
				t.Errorf("binding.spec.policyName = %q, but policy.metadata.name = %q", bindingRef, policyName)
			}
		})
	}
}

func TestPolicyBindingsHaveDenyAction(t *testing.T) {
	root := findProjectRoot(t)
	policyDir := filepath.Join(root, "deploy", "prod", "policies")

	files := []string{
		"require-queue-assignment.yaml",
		"deny-direct-jobset.yaml",
		"deny-direct-workload.yaml",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			docs := parsePolicyFile(t, filepath.Join(policyDir, f))

			for _, doc := range docs {
				if doc.Kind != "ValidatingAdmissionPolicyBinding" {
					continue
				}
				hasDeny := false
				for _, a := range doc.Spec.ValidationActions {
					if a == "Deny" {
						hasDeny = true
					}
				}
				if !hasDeny {
					t.Errorf("binding %q missing Deny validationAction", doc.Metadata.Name)
				}
			}
		})
	}
}

func TestPoliciesTargetManagedNamespaces(t *testing.T) {
	root := findProjectRoot(t)
	policyDir := filepath.Join(root, "deploy", "prod", "policies")

	files := []string{
		"require-queue-assignment.yaml",
		"deny-direct-jobset.yaml",
		"deny-direct-workload.yaml",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			docs := parsePolicyFile(t, filepath.Join(policyDir, f))

			for _, doc := range docs {
				if doc.Kind != "ValidatingAdmissionPolicy" {
					continue
				}

				hasManagedCondition := false
				for _, mc := range doc.Spec.MatchConditions {
					if mc.Name == "managed-namespace" {
						hasManagedCondition = true
						if !strings.Contains(mc.Expression, "rtj.checkpoint.example.io/managed") {
							t.Errorf("managed-namespace matchCondition does not reference rtj.checkpoint.example.io/managed label")
						}
					}
				}
				if !hasManagedCondition {
					t.Errorf("policy %q missing managed-namespace matchCondition", doc.Metadata.Name)
				}
			}
		})
	}
}

func TestPoliciesFailClosed(t *testing.T) {
	root := findProjectRoot(t)
	policyDir := filepath.Join(root, "deploy", "prod", "policies")

	files := []string{
		"require-queue-assignment.yaml",
		"deny-direct-jobset.yaml",
		"deny-direct-workload.yaml",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			docs := parsePolicyFile(t, filepath.Join(policyDir, f))

			for _, doc := range docs {
				if doc.Kind != "ValidatingAdmissionPolicy" {
					continue
				}
				if doc.Spec.FailurePolicy != "Fail" {
					t.Errorf("policy %q failurePolicy = %q, want Fail", doc.Metadata.Name, doc.Spec.FailurePolicy)
				}
			}
		})
	}
}

func TestQueueAssignmentPolicyTargetsRTJ(t *testing.T) {
	root := findProjectRoot(t)
	docs := parsePolicyFile(t, filepath.Join(root, "deploy", "prod", "policies", "require-queue-assignment.yaml"))

	for _, doc := range docs {
		if doc.Kind != "ValidatingAdmissionPolicy" {
			continue
		}
		if doc.Spec.MatchConstraints == nil || len(doc.Spec.MatchConstraints.ResourceRules) == 0 {
			t.Fatal("policy has no matchConstraints.resourceRules")
		}

		rule := doc.Spec.MatchConstraints.ResourceRules[0]
		if len(rule.APIGroups) == 0 || rule.APIGroups[0] != "training.checkpoint.example.io" {
			t.Errorf("resourceRule apiGroup = %v, want [training.checkpoint.example.io]", rule.APIGroups)
		}
		if len(rule.Resources) == 0 || rule.Resources[0] != "resumabletrainingjobs" {
			t.Errorf("resourceRule resources = %v, want [resumabletrainingjobs]", rule.Resources)
		}

		// Validation should check queueName
		if len(doc.Spec.Validations) == 0 {
			t.Fatal("policy has no validations")
		}
		if !strings.Contains(doc.Spec.Validations[0].Expression, "queueName") {
			t.Error("validation expression does not reference queueName")
		}
	}
}

func TestDenyJobsetPolicyExemptsController(t *testing.T) {
	root := findProjectRoot(t)
	docs := parsePolicyFile(t, filepath.Join(root, "deploy", "prod", "policies", "deny-direct-jobset.yaml"))

	for _, doc := range docs {
		if doc.Kind != "ValidatingAdmissionPolicy" {
			continue
		}

		hasControllerExemption := false
		for _, mc := range doc.Spec.MatchConditions {
			if mc.Name == "not-controller" {
				hasControllerExemption = true
				if !strings.Contains(mc.Expression, "system:serviceaccount:rtj-system:") {
					t.Error("not-controller condition does not reference rtj-system service accounts")
				}
			}
		}
		if !hasControllerExemption {
			t.Error("deny-direct-jobset policy missing not-controller matchCondition")
		}
	}
}

func TestDenyWorkloadPolicyExemptsControllerAndKueue(t *testing.T) {
	root := findProjectRoot(t)
	docs := parsePolicyFile(t, filepath.Join(root, "deploy", "prod", "policies", "deny-direct-workload.yaml"))

	for _, doc := range docs {
		if doc.Kind != "ValidatingAdmissionPolicy" {
			continue
		}

		hasRTJExemption := false
		hasKueueExemption := false
		for _, mc := range doc.Spec.MatchConditions {
			if mc.Name == "not-rtj-controller" {
				hasRTJExemption = true
				if !strings.Contains(mc.Expression, "system:serviceaccount:rtj-system:") {
					t.Error("not-rtj-controller condition does not reference rtj-system")
				}
			}
			if mc.Name == "not-kueue-controller" {
				hasKueueExemption = true
				if !strings.Contains(mc.Expression, "system:serviceaccount:kueue-system:") {
					t.Error("not-kueue-controller condition does not reference kueue-system")
				}
			}
		}
		if !hasRTJExemption {
			t.Error("deny-direct-workload policy missing not-rtj-controller matchCondition")
		}
		if !hasKueueExemption {
			t.Error("deny-direct-workload policy missing not-kueue-controller matchCondition")
		}
	}
}

// ---------------------------------------------------------------------------
// RBAC minimization tests
// ---------------------------------------------------------------------------

type rbacRole struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Rules []rbacRule `json:"rules"`
}

type rbacRule struct {
	APIGroups []string `json:"apiGroups"`
	Resources []string `json:"resources"`
	Verbs     []string `json:"verbs"`
}

func parseRBACFile(t *testing.T, path string) []rbacRole {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}

	var roles []rbacRole
	for _, raw := range strings.Split(string(data), "---") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var role rbacRole
		if err := yaml.Unmarshal([]byte(raw), &role); err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}
		if role.Kind != "" {
			roles = append(roles, role)
		}
	}
	return roles
}

func TestControllerRBACDoesNotCreateRTJ(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "role.yaml"))

	for _, role := range roles {
		if role.Kind != "ClusterRole" {
			continue
		}
		for _, rule := range role.Rules {
			isRTJRule := false
			for _, res := range rule.Resources {
				if res == "resumabletrainingjobs" {
					isRTJRule = true
				}
			}
			if !isRTJRule {
				continue
			}
			for _, verb := range rule.Verbs {
				if verb == "create" {
					t.Error("controller ClusterRole should not have 'create' on resumabletrainingjobs")
				}
			}
		}
	}
}

func TestControllerRBACDoesNotUpdateEvents(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "role.yaml"))

	for _, role := range roles {
		if role.Kind != "ClusterRole" {
			continue
		}
		for _, rule := range role.Rules {
			isEventsRule := false
			for _, res := range rule.Resources {
				if res == "events" {
					isEventsRule = true
				}
			}
			if !isEventsRule {
				continue
			}
			for _, verb := range rule.Verbs {
				if verb == "update" {
					t.Error("controller ClusterRole should not have 'update' on events")
				}
			}
		}
	}
}

func TestControllerRBACHasRequiredPermissions(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "role.yaml"))

	// Find the manager ClusterRole
	var managerRole *rbacRole
	for i, role := range roles {
		if role.Kind == "ClusterRole" {
			managerRole = &roles[i]
			break
		}
	}
	if managerRole == nil {
		t.Fatal("no ClusterRole found in role.yaml")
	}

	// Required permission sets: resource -> minimum verbs
	required := map[string][]string{
		"resumabletrainingjobs":          {"get", "list", "watch", "update", "patch"},
		"resumabletrainingjobs/status":   {"get", "update", "patch"},
		"resumabletrainingjobs/finalizers": {"update"},
		"jobsets":                        {"get", "list", "watch", "create", "update", "patch", "delete"},
		"workloads":                      {"get", "list", "watch", "create", "update", "patch", "delete"},
		"configmaps":                     {"get", "list", "watch", "create", "update", "patch"},
		"events":                         {"create", "patch"},
	}

	foundVerbs := make(map[string]map[string]bool)
	for _, rule := range managerRole.Rules {
		for _, res := range rule.Resources {
			if _, ok := foundVerbs[res]; !ok {
				foundVerbs[res] = make(map[string]bool)
			}
			for _, verb := range rule.Verbs {
				foundVerbs[res][verb] = true
			}
		}
	}

	for resource, verbs := range required {
		for _, verb := range verbs {
			if !foundVerbs[resource][verb] {
				t.Errorf("controller role missing %s on %s", verb, resource)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// User-facing RBAC role tests
// ---------------------------------------------------------------------------

func TestUserRolesExist(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "rtj_user_roles.yaml"))

	foundEditor := false
	foundViewer := false
	for _, role := range roles {
		if role.Kind != "ClusterRole" {
			continue
		}
		switch role.Metadata.Name {
		case "rtj-editor":
			foundEditor = true
		case "rtj-viewer":
			foundViewer = true
		}
	}

	if !foundEditor {
		t.Error("rtj-editor ClusterRole not found")
	}
	if !foundViewer {
		t.Error("rtj-viewer ClusterRole not found")
	}
}

func TestEditorRoleHasRTJCRUD(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "rtj_user_roles.yaml"))

	var editor *rbacRole
	for i, role := range roles {
		if role.Metadata.Name == "rtj-editor" {
			editor = &roles[i]
			break
		}
	}
	if editor == nil {
		t.Fatal("rtj-editor not found")
	}

	requiredVerbs := []string{"get", "list", "watch", "create", "update", "patch", "delete"}
	rtjVerbs := make(map[string]bool)
	for _, rule := range editor.Rules {
		for _, res := range rule.Resources {
			if res == "resumabletrainingjobs" {
				for _, verb := range rule.Verbs {
					rtjVerbs[verb] = true
				}
			}
		}
	}

	for _, v := range requiredVerbs {
		if !rtjVerbs[v] {
			t.Errorf("rtj-editor missing verb %q on resumabletrainingjobs", v)
		}
	}
}

func TestEditorRoleCannotWriteJobSets(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "rtj_user_roles.yaml"))

	var editor *rbacRole
	for i, role := range roles {
		if role.Metadata.Name == "rtj-editor" {
			editor = &roles[i]
			break
		}
	}
	if editor == nil {
		t.Fatal("rtj-editor not found")
	}

	writeVerbs := map[string]bool{"create": true, "update": true, "patch": true, "delete": true}
	for _, rule := range editor.Rules {
		for _, res := range rule.Resources {
			if res == "jobsets" {
				for _, verb := range rule.Verbs {
					if writeVerbs[verb] {
						t.Errorf("rtj-editor should not have %q on jobsets", verb)
					}
				}
			}
		}
	}
}

func TestViewerRoleIsReadOnly(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "rtj_user_roles.yaml"))

	var viewer *rbacRole
	for i, role := range roles {
		if role.Metadata.Name == "rtj-viewer" {
			viewer = &roles[i]
			break
		}
	}
	if viewer == nil {
		t.Fatal("rtj-viewer not found")
	}

	readOnlyVerbs := map[string]bool{"get": true, "list": true, "watch": true}
	for _, rule := range viewer.Rules {
		for _, verb := range rule.Verbs {
			if !readOnlyVerbs[verb] {
				t.Errorf("rtj-viewer has write verb %q on %v", verb, rule.Resources)
			}
		}
	}
}

func TestUserRolesHaveAggregationLabels(t *testing.T) {
	root := findProjectRoot(t)
	roles := parseRBACFile(t, filepath.Join(root, "config", "rbac", "rtj_user_roles.yaml"))

	for _, role := range roles {
		if role.Kind != "ClusterRole" {
			continue
		}
		switch role.Metadata.Name {
		case "rtj-editor":
			if role.Metadata.Labels["rbac.authorization.k8s.io/aggregate-to-edit"] != "true" {
				t.Error("rtj-editor missing aggregate-to-edit label")
			}
		case "rtj-viewer":
			if role.Metadata.Labels["rbac.authorization.k8s.io/aggregate-to-view"] != "true" {
				t.Error("rtj-viewer missing aggregate-to-view label")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Namespace manifest tests
// ---------------------------------------------------------------------------

func TestManagedNamespaceHasRequiredLabels(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "namespaces", "managed-namespace.yaml"))
	if err != nil {
		t.Fatalf("read managed-namespace.yaml: %v", err)
	}

	var ns struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
	}

	if err := yaml.Unmarshal(data, &ns); err != nil {
		t.Fatalf("parse managed-namespace.yaml: %v", err)
	}

	if ns.Kind != "Namespace" {
		t.Errorf("kind = %q, want Namespace", ns.Kind)
	}
	if ns.Metadata.Labels["rtj.checkpoint.example.io/managed"] != "true" {
		t.Error("managed namespace missing rtj.checkpoint.example.io/managed: true label")
	}
	if ns.Metadata.Labels["pod-security.kubernetes.io/enforce"] != "restricted" {
		t.Error("managed namespace should enforce PSS restricted")
	}
}

// ---------------------------------------------------------------------------
// Tenancy overlay tests
// ---------------------------------------------------------------------------

func TestTenancyOverlayExists(t *testing.T) {
	root := findProjectRoot(t)
	overlayDir := filepath.Join(root, "deploy", "prod", "overlays", "tenancy")

	requiredFiles := []string{
		"kustomization.yaml",
	}

	for _, f := range requiredFiles {
		path := filepath.Join(overlayDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("tenancy overlay file missing: %s", f)
		}
	}
}

func TestTenancyOverlayReferencesAllPolicies(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "overlays", "tenancy", "kustomization.yaml"))
	if err != nil {
		t.Fatalf("read tenancy kustomization.yaml: %v", err)
	}

	content := string(data)
	requiredRefs := []string{
		"require-queue-assignment.yaml",
		"deny-direct-jobset.yaml",
		"deny-direct-workload.yaml",
		"rtj_user_roles.yaml",
	}

	for _, ref := range requiredRefs {
		if !strings.Contains(content, ref) {
			t.Errorf("tenancy kustomization.yaml missing reference to %s", ref)
		}
	}
}

// ---------------------------------------------------------------------------
// ClusterQueue example tests
// ---------------------------------------------------------------------------

func TestClusterQueueExampleHasNamespaceSelector(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "namespaces", "clusterqueue-example.yaml"))
	if err != nil {
		t.Fatalf("read clusterqueue-example.yaml: %v", err)
	}

	// Parse multi-doc YAML; find the ClusterQueue
	for _, raw := range strings.Split(string(data), "---") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var doc struct {
			Kind string `json:"kind"`
			Spec struct {
				NamespaceSelector struct {
					MatchLabels map[string]string `json:"matchLabels"`
				} `json:"namespaceSelector"`
			} `json:"spec"`
		}

		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatalf("parse clusterqueue doc: %v", err)
		}

		if doc.Kind != "ClusterQueue" {
			continue
		}

		if doc.Spec.NamespaceSelector.MatchLabels["rtj.checkpoint.example.io/managed"] != "true" {
			t.Error("ClusterQueue namespaceSelector should match rtj.checkpoint.example.io/managed: true")
		}
		return
	}

	t.Fatal("no ClusterQueue found in clusterqueue-example.yaml")
}
