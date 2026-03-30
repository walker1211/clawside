package orchestrator

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

func NewID(prefix string) string {
	id, err := uuid.NewV7()
	if err != nil {
		return fallbackID(prefix, time.Now().UTC())
	}
	return fmt.Sprintf("%s_%s", prefix, id.String())
}

func fallbackID(prefix string, now time.Time) string {
	return fmt.Sprintf("%s_%d", prefix, now.UTC().UnixNano())
}
