# Phase 10 Open Questions

Tracking unresolved design questions for Phase 10.

---

## OQ-10.01 - Conversion Webhook Deployment Strategy

**Question:** Should the conversion webhook be served by the operator pod
(same binary, same port 9443) or by a separate deployment?

**Options:**
1. **Same pod (recommended):** Simpler deployment, single binary, shared TLS
   certificate. The webhook server already runs in the operator pod for
   mutating/validating webhooks. Adding conversion is a single handler
   registration. Downside: operator restart = brief conversion unavailability.
2. **Separate deployment:** Independent scaling, independent lifecycle. Adds
   operational complexity and another TLS certificate to manage.

**Leaning:** Option 1 (same pod). The conversion webhook is lightweight and
the operator already handles mutating/validating webhooks on the same port.

**Blocked by:** Nothing. Decision needed before implementation begins.

---

## OQ-10.02 - v1alpha1 Deprecation Timeline

**Question:** When should v1alpha1 be removed from served versions?

**Options:**
1. **Phase 10:** Remove v1alpha1 served version immediately after
   StorageVersionMigration. Aggressive but clean.
2. **Phase 11+ (recommended):** Keep v1alpha1 served for at least 2 releases.
   Users have time to migrate clients. Follows Kubernetes deprecation policy
   spirit.
3. **Never:** Keep v1alpha1 indefinitely as a compatibility layer.

**Leaning:** Option 2. Serve both versions throughout Phase 10; deprecate
v1alpha1 serving in Phase 11 with a clear migration window.

**Blocked by:** Nothing. Timeline decision, not implementation blocker.

---

## OQ-10.03 - Helm Chart vs Kustomize as Primary

**Question:** Should the Helm chart be the sole primary install path, or
should Helm and Kustomize production overlays be co-primary?

**Options:**
1. **Helm primary, Kustomize reference (recommended):** Helm is the documented
   and tested primary path. Kustomize overlays exist for teams that prefer
   Kustomize but are maintained as secondary.
2. **Co-primary:** Both Helm and Kustomize are fully supported, tested, and
   documented as equals. Higher maintenance burden.

**Leaning:** Option 1. Helm as primary reduces the testing and documentation
surface while still supporting Kustomize users.

**Blocked by:** Nothing. Process decision.

---

## OQ-10.04 - Metrics TLS Default

**Question:** Should the metrics endpoint serve TLS by default in production
profiles?

**Options:**
1. **TLS off by default:** Simpler. Metrics scraping within a cluster is often
   plaintext via ServiceMonitor. Users opt in with `--metrics-tls-*` flags.
2. **TLS on by default (recommended):** Defense in depth. cert-manager already
   manages webhook TLS; adding a metrics certificate is low marginal cost.
   Prometheus can be configured to scrape HTTPS.

**Leaning:** Option 2 for production profile; option 1 for dev profile.

**Blocked by:** cert-manager integration design (parallel work).

---

## OQ-10.05 - State Reconstruction Automation Level

**Question:** How automated should disaster recovery state reconstruction be?

**Options:**
1. **Fully manual (recommended for Phase 10):** Provide runbook and tooling
   (scripts/CLI) but require admin to explicitly reconstruct and approve each
   RTJ. Safest approach for initial production release.
2. **Semi-automated:** Controller detects missing RTJs (via orphaned Workloads
   or checkpoint manifests) and creates them in Paused state automatically.
   Requires careful design to avoid false positives.
3. **Fully automated:** Controller auto-reconstructs and auto-resumes. High
   risk of incorrect state propagation.

**Leaning:** Option 1 for Phase 10. Semi-automated reconstruction can be
explored in Phase 11+ after production operational experience.

**Blocked by:** Nothing. Scope decision.

---

## OQ-10.06 - Phase 9 Deferred Metrics Wiring

**Question:** Should Phase 10 wire the Phase 9 metrics that were defined but
not yet called from the reconcile loop?

**Context:** Phase 9 signed off with metrics recorder methods defined but not
called from the reconcile loop. The methods exist; the call sites are missing.

**Options:**
1. **Wire in Phase 10 (recommended):** This is a natural part of production
   hardening. The metrics are already defined and registered; they just need
   call sites in the reconcile path.
2. **Defer to a dedicated metrics wiring task:** Separate concern from Phase 10.

**Leaning:** Option 1. Production observability requires working metrics.

**Blocked by:** Nothing. Implementation task.

---

## OQ-10.07 - Soak Test Infrastructure

**Question:** Where should soak tests run?

**Options:**
1. **Local kind cluster:** Simple, reproducible, but limited scale.
2. **CI-managed cluster (recommended):** Dedicated long-lived cluster in CI
   for soak and chaos testing. More realistic but requires CI infrastructure.
3. **Cloud-provisioned ephemeral cluster:** Spin up on demand, tear down after
   test. Cost-effective but requires cloud provider integration.

**Leaning:** Option 1 for initial implementation; option 2 as a follow-up.

**Blocked by:** CI infrastructure availability.

---

## OQ-10.08 - Feature Gate Persistence

**Question:** How should feature gates be stored and managed?

**Options:**
1. **Command-line flags (current approach):** Simple, already in use for
   `--enable-experimental-partial-admission`. Add new flags for Phase 10
   gates. Requires operator restart to change.
2. **ConfigMap-based:** Store feature gates in a ConfigMap. Controller watches
   for changes and applies dynamically. More complex but enables runtime
   toggling.
3. **Both (recommended):** Flags for immutable gates (API version serving),
   ConfigMap for runtime-toggleable gates (optional features).

**Leaning:** Option 1 for Phase 10 simplicity. ConfigMap approach can be
added later if dynamic toggling is needed.

**Blocked by:** Nothing. Design decision.
