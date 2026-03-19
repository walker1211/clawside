package deliveryrules

import (
	"strings"
	"testing"
)

func TestTelegramMaxTextLength(t *testing.T) {
	if TelegramMaxTextLength != 4096 {
		t.Fatalf("expected telegram max text length 4096, got %d", TelegramMaxTextLength)
	}
}

func TestNormalizeBotName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "trim and lowercase", input: "  Planner  ", want: "planner"},
		{name: "already normalized", input: "engineer", want: "engineer"},
		{name: "blank", input: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeBotName(tt.input); got != tt.want {
				t.Fatalf("NormalizeBotName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapSenderJobStatusToDeliveryStatus(t *testing.T) {
	tests := []struct {
		name        string
		sender      string
		want        string
		wantErrText string
	}{
		{name: "sent", sender: SenderJobStatusSent, want: "sent"},
		{name: "failed", sender: SenderJobStatusFailed, want: "failed"},
		{name: "pending", sender: SenderJobStatusPending, want: "retrying"},
		{name: "sending", sender: SenderJobStatusSending, want: "retrying"},
		{name: "retry", sender: SenderJobStatusRetry, want: "retrying"},
		{name: "unknown", sender: "mystery", wantErrText: "unknown sender job status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MapSenderJobStatusToDeliveryStatus(tt.sender)
			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrText)
				}
				if got != "" {
					t.Fatalf("expected empty status on error, got %q", got)
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrText, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("MapSenderJobStatusToDeliveryStatus(%q) = %q, want %q", tt.sender, got, tt.want)
			}
		})
	}
}
