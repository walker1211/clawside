package a2adelivery

import (
	"fmt"
	"reflect"
)

type TargetUserContext struct {
	DeliveryContextTo       *int64
	DirectSessionPeerChatID *int64
	InboundSenderChatID     *int64
}

func AdaptTargetUserContext(runtimeContext any) (TargetUserContext, error) {
	if runtimeContext == nil {
		return TargetUserContext{}, nil
	}

	switch typed := runtimeContext.(type) {
	case TargetUserContext:
		return typed, nil
	case *TargetUserContext:
		if typed == nil {
			return TargetUserContext{}, nil
		}
		return *typed, nil
	}

	value := reflect.ValueOf(runtimeContext)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return TargetUserContext{}, nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return TargetUserContext{}, fmt.Errorf("runtime context must be TargetUserContext, *TargetUserContext, or same-shape struct with DeliveryContextTo, DirectSessionPeerChatID, and InboundSenderChatID fields of type *int64")
	}

	deliveryContextTo, err := targetUserContextField(value, "DeliveryContextTo")
	if err != nil {
		return TargetUserContext{}, err
	}
	directSessionPeerChatID, err := targetUserContextField(value, "DirectSessionPeerChatID")
	if err != nil {
		return TargetUserContext{}, err
	}
	inboundSenderChatID, err := targetUserContextField(value, "InboundSenderChatID")
	if err != nil {
		return TargetUserContext{}, err
	}

	return TargetUserContext{
		DeliveryContextTo:       deliveryContextTo,
		DirectSessionPeerChatID: directSessionPeerChatID,
		InboundSenderChatID:     inboundSenderChatID,
	}, nil
}

func targetUserContextField(structValue reflect.Value, fieldName string) (*int64, error) {
	field := structValue.FieldByName(fieldName)
	if !field.IsValid() {
		return nil, fmt.Errorf("runtime context must be TargetUserContext, *TargetUserContext, or same-shape struct with DeliveryContextTo, DirectSessionPeerChatID, and InboundSenderChatID fields of type *int64")
	}
	if field.Kind() != reflect.Pointer || field.Type().Elem().Kind() != reflect.Int64 {
		return nil, fmt.Errorf("runtime context field %s must be *int64 for TargetUserContext compatibility", fieldName)
	}
	if field.IsNil() {
		return nil, nil
	}
	value := field.Elem().Int()
	result := int64(value)
	return &result, nil
}

func ResolveTargetUser(explicitChatID *int64, ctx TargetUserContext) (int64, error) {
	if explicitChatID != nil {
		if *explicitChatID <= 0 {
			return 0, fmt.Errorf("explicit chat_id must be positive")
		}
		return *explicitChatID, nil
	}

	deliveryContextTo := ctx.DeliveryContextTo
	directSessionPeerChatID := ctx.DirectSessionPeerChatID
	inboundSenderChatID := ctx.InboundSenderChatID

	if deliveryContextTo != nil {
		if directSessionPeerChatID != nil && *directSessionPeerChatID != *deliveryContextTo {
			return 0, fmt.Errorf("conflicting context chat IDs: deliveryContext.to=%d directSessionPeerChatID=%d", *deliveryContextTo, *directSessionPeerChatID)
		}
		if inboundSenderChatID != nil && *inboundSenderChatID != *deliveryContextTo {
			return 0, fmt.Errorf("conflicting context chat IDs: deliveryContext.to=%d inboundSenderChatID=%d", *deliveryContextTo, *inboundSenderChatID)
		}
		if *deliveryContextTo <= 0 {
			return 0, fmt.Errorf("resolved context chat_id must be positive")
		}
		return *deliveryContextTo, nil
	}

	if directSessionPeerChatID != nil && inboundSenderChatID != nil && *directSessionPeerChatID != *inboundSenderChatID {
		return 0, fmt.Errorf("conflicting context chat IDs: directSessionPeerChatID=%d inboundSenderChatID=%d", *directSessionPeerChatID, *inboundSenderChatID)
	}

	if directSessionPeerChatID != nil {
		if *directSessionPeerChatID <= 0 {
			return 0, fmt.Errorf("resolved context chat_id must be positive")
		}
		return *directSessionPeerChatID, nil
	}
	if inboundSenderChatID != nil {
		if *inboundSenderChatID <= 0 {
			return 0, fmt.Errorf("resolved context chat_id must be positive")
		}
		return *inboundSenderChatID, nil
	}

	return 0, fmt.Errorf("unable to resolve target user chat_id")
}
