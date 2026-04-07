package v1beta1

// Hub marks ResumableTrainingJob as the hub (storage) version for CRD conversion.
// Spoke versions (v1alpha1) implement conversion.Convertible and convert
// to/from this hub type. The API server routes all conversion requests
// through the hub, so adding a new served version only requires a new
// spoke — no N*M conversion matrix.
func (*ResumableTrainingJob) Hub() {}
