# Phase 2 E2E

This note documents the live end-to-end coverage added for the native Kueue Phase 2 path.

## Scope

The e2e suite now covers two high-value live scenarios:

1. native RTJ admission boundary
2. low-priority preemption, graceful yield, checkpoint, and later resume

The tests stay narrow on purpose.
They exercise the current local `kind` profile from [dev-environment.md](dev-environment.md) rather than adding a second environment shape.

## Covered Scenarios

### Native RTJ Admission

`test/e2e/native_kueue_admission_test.go` proves these Phase 2 rules:

- creating an `RTJ` results in one Kueue `Workload` owned by the `RTJ`
- the child `JobSet` is not created while the `RTJ` remains Kueue-suspended
- the child `JobSet` is created only after admission is released
- the child `JobSet` does not carry Kueue queue or priority labels
- no second Kueue `Workload` is created for the child `JobSet`

For deterministic "not admitted yet" behavior, the test uses a dedicated `LocalQueue` with:

```yaml
spec:
  stopPolicy: Hold
```

The queue is then unblocked inside the test.
This is the smallest practical trick that proves "workload exists, but runtime does not launch before admission."

### Priority Preemption And Resume

`test/e2e/priority_preemption_resume_test.go` covers the main Phase 2 story:

1. submit a low-priority `RTJ`
2. wait for it to run
3. submit a higher-priority `RTJ` into the same queue
4. wait for the low-priority `RTJ` to receive a Kueue-driven suspend request
5. verify the operator records a Kueue stop request, waits for checkpoint completion, and tears down the low-priority child `JobSet`
6. verify the high-priority `RTJ` runs
7. delete the high-priority `RTJ` to free quota
8. verify the low-priority `RTJ` is re-admitted and resumes from the recorded checkpoint
9. pause the resumed low-priority `RTJ` once more and verify the later checkpoint step is greater than the preemption checkpoint step

The final re-pause is intentional.
It turns "resume looked plausible" into "resume actually continued training progress from the checkpointed state."

## Manifest Rules

The Phase 2 e2e manifests under `test/e2e/testdata/phase2/` set these fields explicitly:

- `spec.suspend: true`
- `metadata.labels["kueue.x-k8s.io/queue-name"]`
- `metadata.labels["kueue.x-k8s.io/priority-class"]`

That is deliberate for the current live test path.
The tests run the operator locally with `go run ./cmd/operator`; they do not depend on an in-cluster webhook deployment to default `spec.suspend` or project the Kueue labels at create time.

The child JobSet template itself intentionally omits Kueue metadata.
The controller is expected to strip any Kueue management metadata before rendering the runtime object.

## Running The Tests

Prerequisites:

- `make dev-up`
- a trainer image built and loaded into `kind`
- the default `checkpoint-dev` namespace and Phase 2 queue objects present

Run:

```bash
RUN_KIND_E2E=1 \
PAUSE_FLOW_TRAINER_IMAGE=phase1-ddp-counter:dev \
go test ./test/e2e -run 'TestNativeKueueAdmission|TestPriorityPreemptionResume' -count=1
```

`PHASE2_TRAINER_IMAGE` is also accepted and takes precedence over `PAUSE_FLOW_TRAINER_IMAGE`.

## Current Limits

- The tests are live `kind` e2e tests and remain environment-gated.
- They still run the operator as a local process instead of using an in-cluster deployment manifest.
- The suite proves one strong deterministic preemption-resume path; it is not yet a repeated multi-cycle soak.
