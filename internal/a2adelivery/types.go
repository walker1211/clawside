package a2adelivery

// SkillInput defines the fixed-shape input contract for A2A delivery.
// Optional values are represented by nil pointers.
type SkillInput struct {
	TargetAgent    string  `json:"target_agent"`
	Text           string  `json:"text"`
	ChatID         *int64  `json:"chat_id,omitempty"`
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
}

// DeliveryResult defines the fixed-shape output contract for A2A delivery.
// Unknown fields use their type zero values.
type DeliveryResult struct {
	Status       string `json:"status"`
	JobID        int64  `json:"job_id"`
	TargetAgent  string `json:"target_agent"`
	Bot          string `json:"bot"`
	ChatID       int64  `json:"chat_id"`
	AttemptCount int    `json:"attempt_count"`
	LastError    string `json:"last_error"`
}
