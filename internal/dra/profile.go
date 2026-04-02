package dra

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	trainingv1alpha1 "github.com/example/checkpoint-native-preemption-controller/api/v1alpha1"
)

// DeviceProfile is a stable fingerprint of the DRA device requirements
// declared by an RTJ. It captures the set of device classes and CEL
// selectors across all claims in a canonical, order-independent form
// suitable for checkpoint compatibility checks.
//
// Two RTJ specs with the same device profile fingerprint request
// logically equivalent hardware capability, even if individual claim
// names or ordering differ.
type DeviceProfile struct {
	// Fingerprint is the hex-encoded SHA256 hash of the canonical
	// representation of the device profile. Empty when devices are
	// not configured.
	Fingerprint string

	// DeviceClasses is the deduplicated, sorted list of DeviceClass
	// names referenced across all claims.
	DeviceClasses []string

	// canonicalForm is the pre-hash canonical string used for debugging.
	canonicalForm string
}

// BuildProfile constructs a DeviceProfile from an RTJ's device spec.
// Returns a zero-value DeviceProfile when devices are not configured
// (nil spec or mode Disabled).
func BuildProfile(devices *trainingv1alpha1.DeviceSpec) DeviceProfile {
	if devices == nil || devices.Mode != trainingv1alpha1.DeviceModeDRA || len(devices.Claims) == 0 {
		return DeviceProfile{}
	}

	// Collect all device classes (deduplicated) and build canonical
	// claim entries. Each claim contributes a canonical entry:
	//   "class=<deviceClassName>;selectors=<sorted,joined selectors>;count=<count>"
	// The entries are sorted to produce an order-independent canonical form.
	classSet := make(map[string]bool, len(devices.Claims))
	entries := make([]string, 0, len(devices.Claims))

	for _, claim := range devices.Claims {
		classSet[claim.Request.DeviceClassName] = true

		// Sort selectors within each claim for stability.
		sortedSelectors := make([]string, len(claim.Request.Selectors))
		copy(sortedSelectors, claim.Request.Selectors)
		sort.Strings(sortedSelectors)

		entry := fmt.Sprintf("class=%s;selectors=%s;count=%d",
			claim.Request.DeviceClassName,
			strings.Join(sortedSelectors, ","),
			claim.Request.Count,
		)
		entries = append(entries, entry)
	}

	// Sort entries for order independence across claims.
	sort.Strings(entries)
	canonical := strings.Join(entries, "\n")

	// Build sorted device class list.
	classes := make([]string, 0, len(classSet))
	for c := range classSet {
		classes = append(classes, c)
	}
	sort.Strings(classes)

	// Compute SHA256 fingerprint.
	hash := sha256.Sum256([]byte(canonical))
	fingerprint := fmt.Sprintf("%x", hash)

	return DeviceProfile{
		Fingerprint:   fingerprint,
		DeviceClasses: classes,
		canonicalForm: canonical,
	}
}

// IsEmpty returns true when the profile represents no device requirements.
func (p DeviceProfile) IsEmpty() bool {
	return p.Fingerprint == ""
}

// TemplateNameForClaim returns the deterministic ResourceClaimTemplate
// name for a given RTJ name and claim name. The format is:
//
//	<rtj-name>-<claim-name>
func TemplateNameForClaim(rtjName, claimName string) string {
	return fmt.Sprintf("%s-%s", rtjName, claimName)
}

// TemplateRefs builds the sorted list of ResourceClaimTemplateReference
// entries from the RTJ's device claims. The list is deterministic:
// sorted by claim name.
func TemplateRefs(rtjName string, claims []trainingv1alpha1.DeviceClaimSpec) []trainingv1alpha1.ResourceClaimTemplateReference {
	if len(claims) == 0 {
		return nil
	}

	refs := make([]trainingv1alpha1.ResourceClaimTemplateReference, len(claims))
	for i, claim := range claims {
		refs[i] = trainingv1alpha1.ResourceClaimTemplateReference{
			Name:      TemplateNameForClaim(rtjName, claim.Name),
			ClaimName: claim.Name,
		}
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].ClaimName < refs[j].ClaimName
	})
	return refs
}
