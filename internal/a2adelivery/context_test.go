package a2adelivery

import "testing"

type testRuntimeContext struct {
	DeliveryContextTo       *int64
	DirectSessionPeerChatID *int64
	InboundSenderChatID     *int64
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestResolveTargetUserUsesExplicitChatID(t *testing.T) {
	explicitChatID := int64(777001)
	ctx := testRuntimeContext{
		DeliveryContextTo:       int64Ptr(111001),
		DirectSessionPeerChatID: int64Ptr(222001),
		InboundSenderChatID:     int64Ptr(333001),
	}

	got, err := ResolveTargetUser(&explicitChatID, ctx)
	if err != nil {
		t.Fatalf("expected explicit chat_id override to succeed, got error: %v", err)
	}
	if got != explicitChatID {
		t.Fatalf("expected explicit chat_id %d, got %d", explicitChatID, got)
	}
}

func TestResolveTargetUserRejectsNonPositiveExplicitChatID(t *testing.T) {
	ctx := testRuntimeContext{DeliveryContextTo: int64Ptr(111010)}

	explicitZero := int64(0)
	if _, err := ResolveTargetUser(&explicitZero, ctx); err == nil {
		t.Fatalf("expected explicit chat_id=0 to fail")
	}

	explicitNegative := int64(-1)
	if _, err := ResolveTargetUser(&explicitNegative, ctx); err == nil {
		t.Fatalf("expected explicit chat_id<0 to fail")
	}
}

func TestResolveTargetUserUsesContextDeliveryTarget(t *testing.T) {
	t.Run("uses deliveryContext.to as highest priority context", func(t *testing.T) {
		ctx := testRuntimeContext{
			DeliveryContextTo:       int64Ptr(111002),
			DirectSessionPeerChatID: int64Ptr(111002),
			InboundSenderChatID:     int64Ptr(111002),
		}

		got, err := ResolveTargetUser(nil, ctx)
		if err != nil {
			t.Fatalf("expected context resolution to succeed, got error: %v", err)
		}
		if got != 111002 {
			t.Fatalf("expected deliveryContext.to chat_id 111002, got %d", got)
		}
	})

	t.Run("falls back to direct session peer when deliveryContext.to is missing", func(t *testing.T) {
		ctx := testRuntimeContext{
			DeliveryContextTo:       nil,
			DirectSessionPeerChatID: int64Ptr(222002),
			InboundSenderChatID:     int64Ptr(222002),
		}

		got, err := ResolveTargetUser(nil, ctx)
		if err != nil {
			t.Fatalf("expected fallback context resolution to succeed, got error: %v", err)
		}
		if got != 222002 {
			t.Fatalf("expected direct session peer chat_id 222002, got %d", got)
		}
	})
}

func TestResolveTargetUserFailsOnConflictingContext(t *testing.T) {
	ctx := testRuntimeContext{
		DeliveryContextTo:       int64Ptr(111003),
		DirectSessionPeerChatID: int64Ptr(222003),
		InboundSenderChatID:     nil,
	}

	_, err := ResolveTargetUser(nil, ctx)
	if err == nil {
		t.Fatalf("expected conflict error when context sources disagree")
	}
}
