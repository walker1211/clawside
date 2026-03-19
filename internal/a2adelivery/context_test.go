package a2adelivery

import (
	"strings"
	"testing"
)

type testRuntimeContext struct {
	DeliveryContextTo       *int64
	DirectSessionPeerChatID *int64
	InboundSenderChatID     *int64
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestAdaptTargetUserContextNilReturnsZeroValue(t *testing.T) {
	got, err := AdaptTargetUserContext(nil)
	if err != nil {
		t.Fatalf("expected nil runtimeContext to adapt successfully, got error: %v", err)
	}
	if got != (TargetUserContext{}) {
		t.Fatalf("expected zero value TargetUserContext, got %#v", got)
	}
}

func TestAdaptTargetUserContextAcceptsValue(t *testing.T) {
	expected := TargetUserContext{
		DeliveryContextTo:       int64Ptr(111001),
		DirectSessionPeerChatID: int64Ptr(222001),
		InboundSenderChatID:     int64Ptr(333001),
	}

	got, err := AdaptTargetUserContext(expected)
	if err != nil {
		t.Fatalf("expected TargetUserContext value to adapt successfully, got error: %v", err)
	}
	if got != expected {
		t.Fatalf("expected %#v, got %#v", expected, got)
	}
}

func TestAdaptTargetUserContextAcceptsPointer(t *testing.T) {
	expected := TargetUserContext{
		DeliveryContextTo:       int64Ptr(111002),
		DirectSessionPeerChatID: int64Ptr(222002),
		InboundSenderChatID:     int64Ptr(333002),
	}

	got, err := AdaptTargetUserContext(&expected)
	if err != nil {
		t.Fatalf("expected *TargetUserContext to adapt successfully, got error: %v", err)
	}
	if got != expected {
		t.Fatalf("expected %#v, got %#v", expected, got)
	}
}

func TestAdaptTargetUserContextTypedNilPointerReturnsZeroValue(t *testing.T) {
	var typedNil *TargetUserContext

	got, err := AdaptTargetUserContext(typedNil)
	if err != nil {
		t.Fatalf("expected typed nil *TargetUserContext to adapt successfully, got error: %v", err)
	}
	if got != (TargetUserContext{}) {
		t.Fatalf("expected zero value TargetUserContext, got %#v", got)
	}
}

func TestAdaptTargetUserContextAcceptsSameShapeValue(t *testing.T) {
	expected := testRuntimeContext{
		DeliveryContextTo:       int64Ptr(111003),
		DirectSessionPeerChatID: int64Ptr(222003),
		InboundSenderChatID:     int64Ptr(333003),
	}

	got, err := AdaptTargetUserContext(expected)
	if err != nil {
		t.Fatalf("expected same-shape runtimeContext value to adapt successfully, got error: %v", err)
	}
	if got.DeliveryContextTo == nil || *got.DeliveryContextTo != *expected.DeliveryContextTo {
		t.Fatalf("expected DeliveryContextTo %v, got %#v", expected.DeliveryContextTo, got)
	}
	if got.DirectSessionPeerChatID == nil || *got.DirectSessionPeerChatID != *expected.DirectSessionPeerChatID {
		t.Fatalf("expected DirectSessionPeerChatID %v, got %#v", expected.DirectSessionPeerChatID, got)
	}
	if got.InboundSenderChatID == nil || *got.InboundSenderChatID != *expected.InboundSenderChatID {
		t.Fatalf("expected InboundSenderChatID %v, got %#v", expected.InboundSenderChatID, got)
	}
}

func TestAdaptTargetUserContextAcceptsSameShapePointer(t *testing.T) {
	expected := &testRuntimeContext{
		DeliveryContextTo:       int64Ptr(111004),
		DirectSessionPeerChatID: int64Ptr(222004),
		InboundSenderChatID:     int64Ptr(333004),
	}

	got, err := AdaptTargetUserContext(expected)
	if err != nil {
		t.Fatalf("expected same-shape runtimeContext pointer to adapt successfully, got error: %v", err)
	}
	if got.DeliveryContextTo == nil || *got.DeliveryContextTo != *expected.DeliveryContextTo {
		t.Fatalf("expected DeliveryContextTo %v, got %#v", expected.DeliveryContextTo, got)
	}
	if got.DirectSessionPeerChatID == nil || *got.DirectSessionPeerChatID != *expected.DirectSessionPeerChatID {
		t.Fatalf("expected DirectSessionPeerChatID %v, got %#v", expected.DirectSessionPeerChatID, got)
	}
	if got.InboundSenderChatID == nil || *got.InboundSenderChatID != *expected.InboundSenderChatID {
		t.Fatalf("expected InboundSenderChatID %v, got %#v", expected.InboundSenderChatID, got)
	}
}

func TestAdaptTargetUserContextTypedNilSameShapePointerReturnsZeroValue(t *testing.T) {
	var typedNil *testRuntimeContext

	got, err := AdaptTargetUserContext(any(typedNil))
	if err != nil {
		t.Fatalf("expected typed nil same-shape pointer to adapt successfully, got error: %v", err)
	}
	if got != (TargetUserContext{}) {
		t.Fatalf("expected zero value TargetUserContext, got %#v", got)
	}
}

func TestAdaptTargetUserContextRejectsUnsupportedType(t *testing.T) {
	_, err := AdaptTargetUserContext(struct{}{})
	if err == nil {
		t.Fatalf("expected unsupported runtimeContext type to fail")
	}
	if !strings.Contains(err.Error(), "TargetUserContext") {
		t.Fatalf("expected error to mention TargetUserContext, got: %v", err)
	}
}

func TestResolveTargetUserUsesExplicitChatID(t *testing.T) {
	explicitChatID := int64(777001)
	ctx := TargetUserContext{
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

func TestResolveTargetUserUsesContextDeliveryTarget(t *testing.T) {
	ctx := TargetUserContext{
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
}

func TestResolveTargetUserFallsBackToDirectSessionPeer(t *testing.T) {
	ctx := TargetUserContext{
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
}

func TestResolveTargetUserFailsOnConflictingContext(t *testing.T) {
	ctx := TargetUserContext{
		DeliveryContextTo:       int64Ptr(111003),
		DirectSessionPeerChatID: int64Ptr(222003),
		InboundSenderChatID:     nil,
	}

	_, err := ResolveTargetUser(nil, ctx)
	if err == nil {
		t.Fatalf("expected conflict error when context sources disagree")
	}
}

func TestResolveTargetUserRejectsNonPositiveValues(t *testing.T) {
	t.Run("rejects non-positive explicit chat_id", func(t *testing.T) {
		ctx := TargetUserContext{DeliveryContextTo: int64Ptr(111010)}

		explicitZero := int64(0)
		if _, err := ResolveTargetUser(&explicitZero, ctx); err == nil {
			t.Fatalf("expected explicit chat_id=0 to fail")
		}

		explicitNegative := int64(-1)
		if _, err := ResolveTargetUser(&explicitNegative, ctx); err == nil {
			t.Fatalf("expected explicit chat_id<0 to fail")
		}
	})

	t.Run("rejects non-positive resolved context chat_id", func(t *testing.T) {
		ctx := TargetUserContext{DeliveryContextTo: int64Ptr(0)}
		if _, err := ResolveTargetUser(nil, ctx); err == nil {
			t.Fatalf("expected context chat_id=0 to fail")
		}
	})
}

func TestResolveTargetUserFailsWithoutAnyContext(t *testing.T) {
	_, err := ResolveTargetUser(nil, TargetUserContext{})
	if err == nil {
		t.Fatalf("expected missing context to fail")
	}
	if !strings.Contains(err.Error(), "unable to resolve target user chat_id") {
		t.Fatalf("expected unable to resolve error, got: %v", err)
	}
}
