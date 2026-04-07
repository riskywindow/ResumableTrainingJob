package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// findProjectRoot walks up from the test directory to find go.mod.
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// --- Helm chart structure tests ---

func TestHelmChartYAMLExists(t *testing.T) {
	root := findProjectRoot(t)
	chartDir := filepath.Join(root, "charts", "rtj-operator")

	requiredFiles := []string{
		"Chart.yaml",
		"values.yaml",
		"templates/_helpers.tpl",
		"templates/deployment.yaml",
		"templates/serviceaccount.yaml",
		"templates/rbac.yaml",
		"templates/service-webhook.yaml",
		"templates/service-metrics.yaml",
		"templates/webhooks.yaml",
		"templates/cert-manager.yaml",
		"templates/pdb.yaml",
		"templates/networkpolicy.yaml",
	}

	for _, f := range requiredFiles {
		path := filepath.Join(chartDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("required Helm chart file missing: %s", f)
		}
	}
}

func TestHelmChartYAMLParseable(t *testing.T) {
	root := findProjectRoot(t)

	type chartMeta struct {
		APIVersion  string `json:"apiVersion"`
		Name        string `json:"name"`
		Version     string `json:"version"`
		AppVersion  string `json:"appVersion"`
		KubeVersion string `json:"kubeVersion"`
		Type        string `json:"type"`
	}

	data, err := os.ReadFile(filepath.Join(root, "charts", "rtj-operator", "Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}

	var chart chartMeta
	if err := yaml.Unmarshal(data, &chart); err != nil {
		t.Fatalf("parse Chart.yaml: %v", err)
	}

	if chart.APIVersion != "v2" {
		t.Errorf("Chart.apiVersion = %q, want v2", chart.APIVersion)
	}
	if chart.Name != "rtj-operator" {
		t.Errorf("Chart.name = %q, want rtj-operator", chart.Name)
	}
	if chart.Version == "" {
		t.Error("Chart.version is empty")
	}
	if chart.AppVersion == "" {
		t.Error("Chart.appVersion is empty")
	}
	if chart.Type != "application" {
		t.Errorf("Chart.type = %q, want application", chart.Type)
	}
}

func TestHelmValuesDefaults(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "charts", "rtj-operator", "values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatalf("parse values.yaml: %v", err)
	}

	// Verify production defaults
	checks := []struct {
		path string
		want interface{}
	}{
		{"replicaCount", float64(2)},
		{"operatorMode", "worker"},
	}

	for _, c := range checks {
		got, ok := values[c.path]
		if !ok {
			t.Errorf("values.%s is missing", c.path)
			continue
		}
		if got != c.want {
			t.Errorf("values.%s = %v, want %v", c.path, got, c.want)
		}
	}

	// Check leader election is enabled
	le, ok := values["leaderElection"].(map[string]interface{})
	if !ok {
		t.Fatal("values.leaderElection is not a map")
	}
	if le["enabled"] != true {
		t.Errorf("values.leaderElection.enabled = %v, want true", le["enabled"])
	}

	// Check cert-manager is enabled
	cm, ok := values["certManager"].(map[string]interface{})
	if !ok {
		t.Fatal("values.certManager is not a map")
	}
	if cm["enabled"] != true {
		t.Errorf("values.certManager.enabled = %v, want true", cm["enabled"])
	}

	// Check PDB is enabled
	pdb, ok := values["podDisruptionBudget"].(map[string]interface{})
	if !ok {
		t.Fatal("values.podDisruptionBudget is not a map")
	}
	if pdb["enabled"] != true {
		t.Errorf("values.podDisruptionBudget.enabled = %v, want true", pdb["enabled"])
	}

	// Check security context
	psc, ok := values["podSecurityContext"].(map[string]interface{})
	if !ok {
		t.Fatal("values.podSecurityContext is not a map")
	}
	if psc["runAsNonRoot"] != true {
		t.Errorf("values.podSecurityContext.runAsNonRoot = %v, want true", psc["runAsNonRoot"])
	}

	sc, ok := values["securityContext"].(map[string]interface{})
	if !ok {
		t.Fatal("values.securityContext is not a map")
	}
	if sc["allowPrivilegeEscalation"] != false {
		t.Errorf("values.securityContext.allowPrivilegeEscalation = %v, want false", sc["allowPrivilegeEscalation"])
	}
	if sc["readOnlyRootFilesystem"] != true {
		t.Errorf("values.securityContext.readOnlyRootFilesystem = %v, want true", sc["readOnlyRootFilesystem"])
	}
}

// --- Kustomize overlay tests ---

func TestKustomizeProdBaseExists(t *testing.T) {
	root := findProjectRoot(t)
	baseDir := filepath.Join(root, "deploy", "prod", "base")

	requiredFiles := []string{
		"kustomization.yaml",
		"namespace.yaml",
		"patches/manager-prod.yaml",
	}

	for _, f := range requiredFiles {
		path := filepath.Join(baseDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("required prod base file missing: %s", f)
		}
	}
}

func TestKustomizeOverlaysExist(t *testing.T) {
	root := findProjectRoot(t)
	overlays := []struct {
		name  string
		files []string
	}{
		{
			name: "ha",
			files: []string{
				"kustomization.yaml",
				"pdb.yaml",
				"patches/ha-deployment.yaml",
			},
		},
		{
			name: "cert-manager",
			files: []string{
				"kustomization.yaml",
				"issuer.yaml",
				"certificate.yaml",
				"patches/webhook-cainjection.yaml",
			},
		},
		{
			name: "network-policy",
			files: []string{
				"kustomization.yaml",
				"networkpolicy.yaml",
			},
		},
	}

	for _, overlay := range overlays {
		overlayDir := filepath.Join(root, "deploy", "prod", "overlays", overlay.name)
		for _, f := range overlay.files {
			path := filepath.Join(overlayDir, f)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("overlay %s: required file missing: %s", overlay.name, f)
			}
		}
	}
}

// --- cert-manager wiring tests ---

func TestCertManagerCertificateDNSNames(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "overlays", "cert-manager", "certificate.yaml"))
	if err != nil {
		t.Fatalf("read certificate.yaml: %v", err)
	}

	var cert struct {
		Spec struct {
			SecretName string   `json:"secretName"`
			DNSNames   []string `json:"dnsNames"`
			IssuerRef  struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"issuerRef"`
			PrivateKey struct {
				Algorithm string `json:"algorithm"`
			} `json:"privateKey"`
			Usages []string `json:"usages"`
		} `json:"spec"`
	}

	if err := yaml.Unmarshal(data, &cert); err != nil {
		t.Fatalf("parse certificate.yaml: %v", err)
	}

	if cert.Spec.SecretName == "" {
		t.Error("Certificate.spec.secretName is empty")
	}

	if len(cert.Spec.DNSNames) == 0 {
		t.Error("Certificate.spec.dnsNames is empty")
	}

	// Verify at least the short service name and FQDN are present
	hasShort := false
	hasFQDN := false
	for _, dns := range cert.Spec.DNSNames {
		if dns == "webhook-service" {
			hasShort = true
		}
		if strings.HasSuffix(dns, ".svc.cluster.local") {
			hasFQDN = true
		}
	}
	if !hasShort {
		t.Error("Certificate dnsNames missing short service name")
	}
	if !hasFQDN {
		t.Error("Certificate dnsNames missing FQDN (.svc.cluster.local)")
	}

	// Verify issuer reference
	if cert.Spec.IssuerRef.Kind != "Issuer" {
		t.Errorf("Certificate.spec.issuerRef.kind = %q, want Issuer", cert.Spec.IssuerRef.Kind)
	}

	// Verify ECDSA key
	if cert.Spec.PrivateKey.Algorithm != "ECDSA" {
		t.Errorf("Certificate.spec.privateKey.algorithm = %q, want ECDSA", cert.Spec.PrivateKey.Algorithm)
	}

	// Verify server auth usage
	hasServerAuth := false
	for _, u := range cert.Spec.Usages {
		if u == "server auth" {
			hasServerAuth = true
		}
	}
	if !hasServerAuth {
		t.Error("Certificate.spec.usages missing 'server auth'")
	}
}

func TestCertManagerWebhookCAInjection(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "overlays", "cert-manager", "patches", "webhook-cainjection.yaml"))
	if err != nil {
		t.Fatalf("read webhook-cainjection.yaml: %v", err)
	}

	docs := strings.Split(string(data), "---")
	if len(docs) < 2 {
		t.Fatal("webhook-cainjection.yaml should have at least 2 YAML documents (mutating + validating)")
	}

	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		var obj struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("parse webhook-cainjection doc %d: %v", i, err)
		}

		caFrom, ok := obj.Metadata.Annotations["cert-manager.io/inject-ca-from"]
		if !ok {
			t.Errorf("doc %d (%s): missing cert-manager.io/inject-ca-from annotation", i, obj.Kind)
			continue
		}
		if caFrom == "" {
			t.Errorf("doc %d (%s): cert-manager.io/inject-ca-from is empty", i, obj.Kind)
		}
	}
}

// --- Manager YAML hardening tests ---

func TestManagerYAMLSecurity(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "config", "manager", "manager.yaml"))
	if err != nil {
		t.Fatalf("read manager.yaml: %v", err)
	}

	var deploy struct {
		Spec struct {
			Template struct {
				Metadata struct {
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
				Spec struct {
					SecurityContext struct {
						RunAsNonRoot bool `json:"runAsNonRoot"`
						RunAsUser    int  `json:"runAsUser"`
						SeccompProfile struct {
							Type string `json:"type"`
						} `json:"seccompProfile"`
					} `json:"securityContext"`
					Containers []struct {
						Name            string `json:"name"`
						SecurityContext struct {
							AllowPrivilegeEscalation bool `json:"allowPrivilegeEscalation"`
							ReadOnlyRootFilesystem   bool `json:"readOnlyRootFilesystem"`
							Capabilities             struct {
								Drop []string `json:"drop"`
							} `json:"capabilities"`
						} `json:"securityContext"`
						ReadinessProbe interface{} `json:"readinessProbe"`
						LivenessProbe  interface{} `json:"livenessProbe"`
						VolumeMounts   []struct {
							Name      string `json:"name"`
							MountPath string `json:"mountPath"`
							ReadOnly  bool   `json:"readOnly"`
						} `json:"volumeMounts"`
					} `json:"containers"`
					Volumes []struct {
						Name   string `json:"name"`
						Secret struct {
							SecretName string `json:"secretName"`
						} `json:"secret"`
					} `json:"volumes"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}

	if err := yaml.Unmarshal(data, &deploy); err != nil {
		t.Fatalf("parse manager.yaml: %v", err)
	}

	podSec := deploy.Spec.Template.Spec.SecurityContext
	if !podSec.RunAsNonRoot {
		t.Error("manager.yaml: pod securityContext.runAsNonRoot should be true")
	}
	if podSec.SeccompProfile.Type != "RuntimeDefault" {
		t.Errorf("manager.yaml: seccompProfile.type = %q, want RuntimeDefault", podSec.SeccompProfile.Type)
	}

	if len(deploy.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("manager.yaml: no containers found")
	}

	mgr := deploy.Spec.Template.Spec.Containers[0]
	if mgr.SecurityContext.AllowPrivilegeEscalation {
		t.Error("manager.yaml: container allowPrivilegeEscalation should be false")
	}
	if !mgr.SecurityContext.ReadOnlyRootFilesystem {
		t.Error("manager.yaml: container readOnlyRootFilesystem should be true")
	}

	hasDropAll := false
	for _, c := range mgr.SecurityContext.Capabilities.Drop {
		if c == "ALL" {
			hasDropAll = true
		}
	}
	if !hasDropAll {
		t.Error("manager.yaml: container should drop ALL capabilities")
	}

	if mgr.ReadinessProbe == nil {
		t.Error("manager.yaml: readinessProbe is missing")
	}
	if mgr.LivenessProbe == nil {
		t.Error("manager.yaml: livenessProbe is missing")
	}

	// Check webhook cert volume mount
	hasWebhookMount := false
	for _, vm := range mgr.VolumeMounts {
		if vm.Name == "webhook-certs" && vm.ReadOnly {
			hasWebhookMount = true
		}
	}
	if !hasWebhookMount {
		t.Error("manager.yaml: webhook-certs volume mount not found or not read-only")
	}

	// Check webhook cert volume
	hasWebhookVolume := false
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.Name == "webhook-certs" && v.Secret.SecretName != "" {
			hasWebhookVolume = true
		}
	}
	if !hasWebhookVolume {
		t.Error("manager.yaml: webhook-certs volume not found")
	}
}

// --- HA overlay tests ---

func TestHAOverlayDeploymentReplicas(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "overlays", "ha", "patches", "ha-deployment.yaml"))
	if err != nil {
		t.Fatalf("read ha-deployment.yaml: %v", err)
	}

	var deploy struct {
		Spec struct {
			Replicas int `json:"replicas"`
			Template struct {
				Spec struct {
					Affinity struct {
						PodAntiAffinity struct {
							Required []struct {
								TopologyKey string `json:"topologyKey"`
							} `json:"requiredDuringSchedulingIgnoredDuringExecution"`
						} `json:"podAntiAffinity"`
					} `json:"affinity"`
					TopologySpreadConstraints []struct {
						MaxSkew     int    `json:"maxSkew"`
						TopologyKey string `json:"topologyKey"`
					} `json:"topologySpreadConstraints"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}

	if err := yaml.Unmarshal(data, &deploy); err != nil {
		t.Fatalf("parse ha-deployment.yaml: %v", err)
	}

	if deploy.Spec.Replicas < 2 {
		t.Errorf("HA deployment replicas = %d, want >= 2", deploy.Spec.Replicas)
	}

	if len(deploy.Spec.Template.Spec.Affinity.PodAntiAffinity.Required) == 0 {
		t.Error("HA deployment should have requiredDuringSchedulingIgnoredDuringExecution anti-affinity")
	}

	if len(deploy.Spec.Template.Spec.TopologySpreadConstraints) == 0 {
		t.Error("HA deployment should have topologySpreadConstraints")
	}
}

func TestHAOverlayPDB(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "overlays", "ha", "pdb.yaml"))
	if err != nil {
		t.Fatalf("read pdb.yaml: %v", err)
	}

	var pdb struct {
		Kind string `json:"kind"`
		Spec struct {
			MinAvailable int `json:"minAvailable"`
			Selector     struct {
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
		} `json:"spec"`
	}

	if err := yaml.Unmarshal(data, &pdb); err != nil {
		t.Fatalf("parse pdb.yaml: %v", err)
	}

	if pdb.Kind != "PodDisruptionBudget" {
		t.Errorf("PDB kind = %q, want PodDisruptionBudget", pdb.Kind)
	}
	if pdb.Spec.MinAvailable < 1 {
		t.Errorf("PDB minAvailable = %d, want >= 1", pdb.Spec.MinAvailable)
	}
}

// --- Network policy test ---

func TestNetworkPolicyStructure(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "prod", "overlays", "network-policy", "networkpolicy.yaml"))
	if err != nil {
		t.Fatalf("read networkpolicy.yaml: %v", err)
	}

	var np struct {
		Kind string `json:"kind"`
		Spec struct {
			PolicyTypes []string `json:"policyTypes"`
			Ingress     []struct {
				Ports []struct {
					Port int `json:"port"`
				} `json:"ports"`
			} `json:"ingress"`
			Egress []struct {
				Ports []struct {
					Port int `json:"port"`
				} `json:"ports"`
			} `json:"egress"`
		} `json:"spec"`
	}

	if err := yaml.Unmarshal(data, &np); err != nil {
		t.Fatalf("parse networkpolicy.yaml: %v", err)
	}

	if np.Kind != "NetworkPolicy" {
		t.Errorf("NetworkPolicy kind = %q, want NetworkPolicy", np.Kind)
	}

	hasIngress := false
	hasEgress := false
	for _, pt := range np.Spec.PolicyTypes {
		if pt == "Ingress" {
			hasIngress = true
		}
		if pt == "Egress" {
			hasEgress = true
		}
	}
	if !hasIngress {
		t.Error("NetworkPolicy should have Ingress policyType")
	}
	if !hasEgress {
		t.Error("NetworkPolicy should have Egress policyType")
	}

	// Verify webhook port (9443) is allowed
	webhookPortAllowed := false
	for _, rule := range np.Spec.Ingress {
		for _, port := range rule.Ports {
			if port.Port == 9443 {
				webhookPortAllowed = true
			}
		}
	}
	if !webhookPortAllowed {
		t.Error("NetworkPolicy should allow ingress on webhook port 9443")
	}

	// Verify DNS egress (port 53) is allowed
	dnsAllowed := false
	for _, rule := range np.Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port == 53 {
				dnsAllowed = true
			}
		}
	}
	if !dnsAllowed {
		t.Error("NetworkPolicy should allow egress on DNS port 53")
	}
}

// --- Helm template content validation (static analysis) ---

func TestHelmDeploymentTemplateContainsModeArg(t *testing.T) {
	root := findProjectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "charts", "rtj-operator", "templates", "deployment.yaml"))
	if err != nil {
		t.Fatalf("read deployment.yaml template: %v", err)
	}

	content := string(data)

	// The template should reference .Values.operatorMode
	if !strings.Contains(content, ".Values.operatorMode") {
		t.Error("deployment.yaml template should reference .Values.operatorMode for manager/worker mode")
	}

	// The template should reference leader election
	if !strings.Contains(content, ".Values.leaderElection") {
		t.Error("deployment.yaml template should reference .Values.leaderElection")
	}

	// The template should reference security contexts
	if !strings.Contains(content, "securityContext") {
		t.Error("deployment.yaml template should contain securityContext")
	}

	// The template should mount webhook certs
	if !strings.Contains(content, "webhook-certs") {
		t.Error("deployment.yaml template should mount webhook-certs volume")
	}
}

func TestHelmCertManagerTemplateContainsCAInjection(t *testing.T) {
	root := findProjectRoot(t)

	// Check webhooks template has cert-manager annotation
	data, err := os.ReadFile(filepath.Join(root, "charts", "rtj-operator", "templates", "webhooks.yaml"))
	if err != nil {
		t.Fatalf("read webhooks.yaml template: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "cert-manager.io/inject-ca-from") {
		t.Error("webhooks.yaml template should contain cert-manager.io/inject-ca-from annotation")
	}

	// Check cert-manager template creates Certificate
	cmData, err := os.ReadFile(filepath.Join(root, "charts", "rtj-operator", "templates", "cert-manager.yaml"))
	if err != nil {
		t.Fatalf("read cert-manager.yaml template: %v", err)
	}

	cmContent := string(cmData)
	if !strings.Contains(cmContent, "kind: Certificate") {
		t.Error("cert-manager.yaml template should contain Certificate resource")
	}
	if !strings.Contains(cmContent, "kind: Issuer") {
		t.Error("cert-manager.yaml template should contain Issuer resource")
	}
	if !strings.Contains(cmContent, "dnsNames") {
		t.Error("cert-manager.yaml template should contain dnsNames")
	}
}
