package jobset

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	QueueLabelKey            = "kueue.x-k8s.io/queue-name"
	WorkloadPriorityLabelKey = "kueue.x-k8s.io/priority-class"
	ManagedByLabelKey        = "training.checkpoint.example.io/managed-by"
	RTJNameLabelKey          = "training.checkpoint.example.io/rtj-name"
	RunAttemptLabelKey       = "training.checkpoint.example.io/run-attempt"
	ControlVolumeName        = "yield-sdk-control"
	StagingVolumeName        = "yield-sdk-staging"
	ControlConfigKey         = "control.json"
	ControlMountDir          = "/var/run/yield-sdk/control"
	ControlFilePath          = ControlMountDir + "/" + ControlConfigKey
	StagingMountDir          = "/var/lib/yield-sdk"
	DefaultStagingRoot       = StagingMountDir + "/staging"
	DefaultRestoreRoot       = StagingMountDir + "/restore"
	DefaultYieldMarkerPath   = StagingMountDir + "/yield-complete.json"
	EnvStorageURI            = "YIELD_SDK_STORAGE_URI"
	EnvControlFile           = "YIELD_SDK_CONTROL_FILE"
	EnvRunAttempt            = "YIELD_SDK_RUN_ATTEMPT"
	EnvRestoreManifestURI    = "YIELD_SDK_RESTORE_MANIFEST_URI"
	EnvStagingRoot           = "YIELD_SDK_STAGING_ROOT"
	EnvRestoreRoot           = "YIELD_SDK_RESTORE_ROOT"
	EnvYieldMarkerPath       = "YIELD_SDK_YIELD_MARKER_PATH"
	EnvYieldMarkerURI        = "YIELD_SDK_YIELD_MARKER_URI"
	EnvRTJIdentity           = "YIELD_SDK_RTJ_IDENTITY"
	EnvClusterIdentity       = "YIELD_SDK_CLUSTER_IDENTITY"
	EnvWorldSize             = "YIELD_SDK_WORLD_SIZE"
	EnvOriginalWorldSize     = "YIELD_SDK_ORIGINAL_WORLD_SIZE"
	EnvAllowWorldSizeChange  = "YIELD_SDK_ALLOW_WORLD_SIZE_CHANGE"
	EnvAdmittedFlavor        = "YIELD_SDK_ADMITTED_FLAVOR"
	EnvTargetWorkerCount     = "YIELD_SDK_TARGET_WORKER_COUNT"
	DefaultClusterIdentity   = "phase1-kind"
	ManagedByLabelValue      = "resumabletrainingjob-controller"
	AdmittedPodSetsAnnotation = "training.checkpoint.example.io/admitted-pod-sets"
)

func ChildJobSetName(rtjName string, runAttempt int32) string {
	return dnsLabel(fmt.Sprintf("%s-run-%d", rtjName, runAttempt))
}

func ControlConfigMapName(rtjName string, runAttempt int32) string {
	return dnsLabel(fmt.Sprintf("%s-run-%d-control", rtjName, runAttempt))
}

func dnsLabel(value string) string {
	clean := strings.ToLower(value)
	clean = strings.ReplaceAll(clean, "_", "-")
	clean = strings.ReplaceAll(clean, ".", "-")
	if len(clean) <= 63 {
		return strings.Trim(clean, "-")
	}

	suffix := strconv.FormatUint(fnv32a(clean), 36)
	maxPrefix := 63 - len(suffix) - 1
	if maxPrefix < 1 {
		maxPrefix = 1
	}
	clean = strings.Trim(clean[:maxPrefix], "-")
	return clean + "-" + suffix
}

func fnv32a(value string) uint64 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	hash := uint64(offset32)
	for _, r := range value {
		hash ^= uint64(r)
		hash *= prime32
	}
	return hash
}
