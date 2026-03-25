package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	webhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
	"sigs.k8s.io/kueue/pkg/podset"
)

const (
	rtjMutatingWebhookPath   = "/mutate-training-checkpoint-example-io-v1alpha1-resumabletrainingjob"
	rtjValidatingWebhookPath = "/validate-training-checkpoint-example-io-v1alpha1-resumabletrainingjob"
)

// +kubebuilder:webhook:path=/mutate-training-checkpoint-example-io-v1alpha1-resumabletrainingjob,mutating=true,failurePolicy=fail,sideEffects=None,groups=training.checkpoint.example.io,resources=resumabletrainingjobs,verbs=create;update,versions=v1alpha1,name=mresumabletrainingjob.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-training-checkpoint-example-io-v1alpha1-resumabletrainingjob,mutating=false,failurePolicy=fail,sideEffects=None,groups=training.checkpoint.example.io,resources=resumabletrainingjobs,verbs=create;update,versions=v1alpha1,name=vresumabletrainingjob.kb.io,admissionReviewVersions=v1

// ResumableTrainingJobWebhook wires the RTJ API to the Kueue webhook helper methods.
// It is intentionally limited to defaulting and validation semantics; the runtime-side
// GenericJob integration is implemented later.
type ResumableTrainingJobWebhook struct {
	Client                       client.Client
	ManageJobsWithoutQueueName   bool
	ManagedJobsNamespaceSelector labels.Selector
	DefaultQueueExist            func(string) bool
}

func (w *ResumableTrainingJobWebhook) defaultQueueExists(namespace string) bool {
	if w.DefaultQueueExist == nil {
		return false
	}
	return w.DefaultQueueExist(namespace)
}

// SetupResumableTrainingJobWebhookWithManager installs the RTJ webhook handlers on the manager webhook server.
func SetupResumableTrainingJobWebhookWithManager(mgr ctrl.Manager) {
	wh := &ResumableTrainingJobWebhook{
		Client:            mgr.GetClient(),
		DefaultQueueExist: func(string) bool { return false },
	}
	server := mgr.GetWebhookServer()
	server.Register(rtjMutatingWebhookPath, &webhook.Admission{Handler: admission.WithCustomDefaulter(mgr.GetScheme(), &ResumableTrainingJob{}, wh)})
	server.Register(rtjValidatingWebhookPath, &webhook.Admission{Handler: admission.WithCustomValidator(mgr.GetScheme(), &ResumableTrainingJob{}, wh)})
}

var _ admission.CustomDefaulter = &ResumableTrainingJobWebhook{}
var _ admission.CustomValidator = &ResumableTrainingJobWebhook{}

func (w *ResumableTrainingJobWebhook) Default(ctx context.Context, obj runtime.Object) error {
	job, err := rtjFromRuntimeObject(obj)
	if err != nil {
		return err
	}
	job.Default()
	return jobframework.ApplyDefaultForSuspend(ctx, newRTJWebhookGenericJob(job), w.Client, w.ManageJobsWithoutQueueName, w.ManagedJobsNamespaceSelector)
}

func (w *ResumableTrainingJobWebhook) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	job, err := rtjFromRuntimeObject(obj)
	if err != nil {
		return nil, err
	}
	jobCopy := job.DeepCopy()
	jobCopy.projectKueueLabels()

	allErrs := jobCopy.validationErrors()
	allErrs = append(allErrs, jobframework.ValidateJobOnCreate(newRTJWebhookGenericJob(jobCopy))...)
	return nil, allErrs.ToAggregate()
}

func (w *ResumableTrainingJobWebhook) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldJob, err := rtjFromRuntimeObject(oldObj)
	if err != nil {
		return nil, err
	}
	newJob, err := rtjFromRuntimeObject(newObj)
	if err != nil {
		return nil, err
	}

	oldCopy := oldJob.DeepCopy()
	newCopy := newJob.DeepCopy()
	oldCopy.projectKueueLabels()
	newCopy.projectKueueLabels()

	oldGeneric := newRTJWebhookGenericJob(oldCopy)
	newGeneric := newRTJWebhookGenericJob(newCopy)

	allErrs := newCopy.validationErrors()
	allErrs = append(allErrs, jobframework.ValidateJobOnCreate(newGeneric)...)
	allErrs = append(allErrs, jobframework.ValidateJobOnUpdate(oldGeneric, newGeneric, w.defaultQueueExists)...)
	return nil, allErrs.ToAggregate()
}

func (w *ResumableTrainingJobWebhook) ValidateDelete(context.Context, runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func rtjFromRuntimeObject(obj runtime.Object) (*ResumableTrainingJob, error) {
	job, ok := obj.(*ResumableTrainingJob)
	if !ok {
		return nil, fmt.Errorf("expected ResumableTrainingJob, got %T", obj)
	}
	return job, nil
}

type rtjWebhookGenericJob struct {
	job *ResumableTrainingJob
}

func newRTJWebhookGenericJob(job *ResumableTrainingJob) jobframework.GenericJob {
	return &rtjWebhookGenericJob{job: job}
}

func (j *rtjWebhookGenericJob) Object() client.Object {
	return j.job
}

func (j *rtjWebhookGenericJob) IsSuspended() bool {
	return j.job.IsSuspendedForKueue()
}

func (j *rtjWebhookGenericJob) Suspend() {
	suspended := true
	j.job.Spec.Suspend = &suspended
}

func (j *rtjWebhookGenericJob) RunWithPodSetsInfo(context.Context, []podset.PodSetInfo) error {
	return nil
}

func (j *rtjWebhookGenericJob) RestorePodSetsInfo([]podset.PodSetInfo) bool {
	return false
}

func (j *rtjWebhookGenericJob) Finished(context.Context) (string, bool, bool) {
	return "", false, false
}

func (j *rtjWebhookGenericJob) PodSets(context.Context) ([]kueue.PodSet, error) {
	return nil, nil
}

func (j *rtjWebhookGenericJob) IsActive() bool {
	return false
}

func (j *rtjWebhookGenericJob) PodsReady(context.Context) bool {
	return false
}

func (j *rtjWebhookGenericJob) GVK() schema.GroupVersionKind {
	return GroupVersion.WithKind("ResumableTrainingJob")
}
