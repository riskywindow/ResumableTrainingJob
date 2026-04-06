package jobset

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kueueconstants "sigs.k8s.io/kueue/pkg/controller/constants"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
	"github.com/example/checkpoint-native-preemption-controller/internal/checkpoints"
	"github.com/example/checkpoint-native-preemption-controller/internal/provisioning"
	"github.com/example/checkpoint-native-preemption-controller/internal/topology"
)

type RenderInput struct {
	RTJ                  *trainingv1alpha1.ResumableTrainingJob
	RunAttempt           int32
	JobSetName           string
	ControlConfigMapName string
	ResumeManifestURI    string

	// Phase 3: admitted shape from Kueue. When non-nil, the renderer adjusts
	// replica counts for each replicated job based on admitted pod counts.
	// Keys are replicated job names, values are admitted pod counts.
	AdmittedCounts map[string]int32

	// Phase 3: original world size from spec.identity.worldSize.
	// When > 0, Phase 3 env vars (YIELD_SDK_WORLD_SIZE, etc.) are injected.
	OriginalWorldSize int32

	// Phase 3: whether world size change is allowed.
	AllowWorldSizeChange bool

	// Phase 3: admitted flavor name for observability (optional).
	AdmittedFlavor string

	// Phase 4: parsed topology assignment from Kueue TAS. When non-nil,
	// topology constraints (nodeSelector) are injected into pod templates.
	TopologyResult *topology.ParseResult

	// Phase 7: merged podSetUpdates from AdmissionCheck suggestions.
	// When non-nil, these updates are applied additively to the rendered
	// JobSet after topology injection. Keys are PodSet names.
	PodSetUpdates map[string]provisioning.PodSetUpdateEntry

	// Phase 8: DRA claim injections. When non-nil, DRA resource claims
	// are injected into the rendered JobSet pod templates after all other
	// injections (topology, podSetUpdates). Each entry maps to one
	// DeviceClaimSpec from the RTJ with its resolved template name.
	DRAClaims []DRAClaimInjection

	// Phase 9: elastic target worker count. When > 0, the
	// YIELD_SDK_TARGET_WORKER_COUNT env var is injected into all containers
	// so the runtime can observe the desired worker count for resize coordination.
	ElasticTargetWorkerCount int32
}

func RenderChildJobSet(input RenderInput) (*Object, error) {
	spec, err := ParseTemplate(input.RTJ.Spec.Runtime.Template.Spec)
	if err != nil {
		return nil, err
	}

	obj := &Object{
		APIVersion: input.RTJ.Spec.Runtime.Template.APIVersion,
		Kind:       input.RTJ.Spec.Runtime.Template.Kind,
		Metadata: metav1.ObjectMeta{
			Name:        input.JobSetName,
			Namespace:   input.RTJ.Namespace,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: spec,
	}

	if input.RTJ.Spec.Runtime.Template.Metadata != nil {
		for key, value := range input.RTJ.Spec.Runtime.Template.Metadata.Labels {
			obj.Metadata.Labels[key] = value
		}
		for key, value := range input.RTJ.Spec.Runtime.Template.Metadata.Annotations {
			obj.Metadata.Annotations[key] = value
		}
	}

	stripKueueManagementMetadata(&obj.Metadata)
	obj.Metadata.Labels[ManagedByLabelKey] = ManagedByLabelValue
	obj.Metadata.Labels[RTJNameLabelKey] = input.RTJ.Name
	obj.Metadata.Labels[RunAttemptLabelKey] = strconv.Itoa(int(input.RunAttempt))

	for index := range obj.Spec.ReplicatedJobs {
		replicatedJob := &obj.Spec.ReplicatedJobs[index]

		// Phase 3: apply admitted replica counts from Kueue admission.
		if input.AdmittedCounts != nil {
			if count, ok := input.AdmittedCounts[replicatedJob.Name]; ok {
				applyAdmittedReplicaCount(replicatedJob, count)
			}
		}

		// Phase 3: strip Kueue management labels from pod templates.
		// podset.Merge in RunWithPodSetsInfo may inject Kueue-prefixed
		// labels/annotations; they must not reach the child JobSet pods.
		stripKueuePodTemplateLabels(replicatedJob)

		pod := podSpec(replicatedJob)
		ensureVolume(pod, corev1.Volume{
			Name: ControlVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: input.ControlConfigMapName},
					Items: []corev1.KeyToPath{
						{Key: ControlConfigKey, Path: ControlConfigKey},
					},
				},
			},
		})
		ensureVolume(pod, corev1.Volume{
			Name:         StagingVolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})

		for containerIndex := range pod.Containers {
			container := &pod.Containers[containerIndex]
			upsertEnv(container, EnvStorageURI, input.RTJ.Spec.Checkpoint.StorageURI)
			upsertEnv(container, EnvControlFile, ControlFilePath)
			upsertEnv(container, EnvRunAttempt, strconv.Itoa(int(input.RunAttempt)))
			upsertEnv(container, EnvStagingRoot, DefaultStagingRoot)
			upsertEnv(container, EnvRestoreRoot, DefaultRestoreRoot)
			upsertEnv(container, EnvYieldMarkerPath, DefaultYieldMarkerPath)
			upsertEnv(container, EnvYieldMarkerURI, checkpoints.YieldMarkerURI(input.RTJ.Spec.Checkpoint.StorageURI, input.RunAttempt))
			upsertEnv(container, EnvRTJIdentity, input.RTJ.Name)
			upsertEnv(container, EnvClusterIdentity, DefaultClusterIdentity)
			if input.ResumeManifestURI != "" {
				upsertEnv(container, EnvRestoreManifestURI, input.ResumeManifestURI)
			}

			// Phase 9: inject elastic target worker count when set.
			if input.ElasticTargetWorkerCount > 0 {
				upsertEnv(container, EnvTargetWorkerCount, strconv.Itoa(int(input.ElasticTargetWorkerCount)))
			}

			// Phase 3: inject world-size and flavor env vars when admission is active.
			if input.OriginalWorldSize > 0 {
				effectiveWorldSize := computeEffectiveWorldSize(input.AdmittedCounts, input.OriginalWorldSize)
				upsertEnv(container, EnvWorldSize, strconv.Itoa(int(effectiveWorldSize)))
				upsertEnv(container, EnvOriginalWorldSize, strconv.Itoa(int(input.OriginalWorldSize)))
				upsertEnv(container, EnvAllowWorldSizeChange, strconv.FormatBool(input.AllowWorldSizeChange))
				if input.AdmittedFlavor != "" {
					upsertEnv(container, EnvAdmittedFlavor, input.AdmittedFlavor)
				}
			}

			ensureVolumeMount(container, corev1.VolumeMount{
				Name:      ControlVolumeName,
				MountPath: ControlMountDir,
				ReadOnly:  true,
			})
			ensureVolumeMount(container, corev1.VolumeMount{
				Name:      StagingVolumeName,
				MountPath: StagingMountDir,
			})
		}
	}

	// Phase 4: inject topology constraints into pod templates when topology
	// assignment is available.
	if input.TopologyResult != nil {
		workerName := resolveWorkerName(input.RTJ, &obj.Spec)
		if _, err := InjectTopology(&obj.Spec, workerName, input.TopologyResult); err != nil {
			return nil, fmt.Errorf("inject topology: %w", err)
		}
	}

	// Phase 7: apply podSetUpdates from AdmissionCheck suggestions additively.
	if len(input.PodSetUpdates) > 0 {
		updateResult := ApplyPodSetUpdates(&obj.Spec, input.PodSetUpdates)
		if !updateResult.Applied {
			return nil, fmt.Errorf("apply podSetUpdates: %s", updateResult.ConflictMessage())
		}
	}

	// Phase 8: inject DRA resource claims into pod templates. This is
	// the last injection step so that DRA claims compose cleanly with
	// topology constraints and podSetUpdates.
	if len(input.DRAClaims) > 0 {
		InjectDRAClaims(&obj.Spec, input.DRAClaims)
	}

	return obj, nil
}

// resolveWorkerName determines the worker PodSet name for topology injection.
func resolveWorkerName(rtj *trainingv1alpha1.ResumableTrainingJob, spec *Spec) string {
	name := rtj.EffectivePodSetName()
	if name == "" && len(spec.ReplicatedJobs) > 0 {
		name = spec.ReplicatedJobs[0].Name
	}
	return name
}

func stripKueueManagementMetadata(meta *metav1.ObjectMeta) {
	if meta == nil {
		return
	}

	for key := range meta.Labels {
		if isKueueManagementLabel(key) {
			delete(meta.Labels, key)
		}
	}
	for key := range meta.Annotations {
		if isKueueManagementAnnotation(key) {
			delete(meta.Annotations, key)
		}
	}
}

func isKueueManagementLabel(key string) bool {
	return strings.HasPrefix(key, "kueue.x-k8s.io/") ||
		key == kueueconstants.QueueLabel ||
		key == kueueconstants.WorkloadPriorityClassLabel ||
		key == kueueconstants.PrebuiltWorkloadLabel ||
		key == kueueconstants.MaxExecTimeSecondsLabel
}

func isKueueManagementAnnotation(key string) bool {
	return strings.HasPrefix(key, "kueue.x-k8s.io/") ||
		strings.HasPrefix(key, kueueconstants.ProvReqAnnotationPrefix)
}

func ensureVolume(pod *corev1.PodSpec, volume corev1.Volume) {
	for index := range pod.Volumes {
		if pod.Volumes[index].Name == volume.Name {
			pod.Volumes[index] = volume
			return
		}
	}
	pod.Volumes = append(pod.Volumes, volume)
}

func ensureVolumeMount(container *corev1.Container, mount corev1.VolumeMount) {
	for index := range container.VolumeMounts {
		if container.VolumeMounts[index].Name == mount.Name {
			container.VolumeMounts[index] = mount
			return
		}
	}
	container.VolumeMounts = append(container.VolumeMounts, mount)
}

func upsertEnv(container *corev1.Container, name, value string) {
	for index := range container.Env {
		if container.Env[index].Name == name {
			container.Env[index].Value = value
			container.Env[index].ValueFrom = nil
			return
		}
	}
	container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
}

// computeEffectiveWorldSize returns the total admitted pod count when admission
// data is available, falling back to the original world size.
func computeEffectiveWorldSize(admittedCounts map[string]int32, originalWorldSize int32) int32 {
	if len(admittedCounts) > 0 {
		var total int32
		for _, count := range admittedCounts {
			total += count
		}
		if total > 0 {
			return total
		}
	}
	return originalWorldSize
}

func RenderChildJobSetUnstructured(input RenderInput) (*unstructured.Unstructured, error) {
	obj, err := RenderChildJobSet(input)
	if err != nil {
		return nil, fmt.Errorf("render child JobSet: %w", err)
	}
	return ToUnstructured(*obj)
}
