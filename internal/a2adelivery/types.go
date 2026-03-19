package a2adelivery

// SkillInput defines the fixed-shape input contract for A2A delivery.
// Optional values are represented by nil pointers.
type SkillInput struct {
	TargetAgent    string
	Text           string
	ChatID         *int64
	IdempotencyKey *string
}

// DeliveryResult defines the fixed-shape output contract for A2A delivery.
// Unknown fields use their type zero values.
type DeliveryResult struct {
	Status       string
	JobID        int64
	TargetAgent  string
	Bot          string
	ChatID       int64
	AttemptCount int
	LastError    string
}
