package a2adelivery

import (
	"fmt"
	"reflect"
)

func ResolveTargetUser(explicitChatID *int64, runtimeContext any) (int64, error) {
	if explicitChatID != nil {
		if *explicitChatID <= 0 {
			return 0, fmt.Errorf("explicit chat_id must be non-zero")
		}
		return *explicitChatID, nil
	}

	deliveryContextTo, directSessionPeerChatID, inboundSenderChatID, err := extractContextChatIDs(runtimeContext)
	if err != nil {
		return 0, err
	}

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

func extractContextChatIDs(runtimeContext any) (deliveryContextTo *int64, directSessionPeerChatID *int64, inboundSenderChatID *int64, err error) {
	if runtimeContext == nil {
		return nil, nil, nil, nil
	}

	value := reflect.ValueOf(runtimeContext)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil, nil, nil
		}
		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return nil, nil, nil, fmt.Errorf("runtime context must be a struct or pointer to struct")
	}

	deliveryContextTo, err = int64PtrFromField(value, "DeliveryContextTo")
	if err != nil {
		return nil, nil, nil, err
	}
	directSessionPeerChatID, err = int64PtrFromField(value, "DirectSessionPeerChatID")
	if err != nil {
		return nil, nil, nil, err
	}
	inboundSenderChatID, err = int64PtrFromField(value, "InboundSenderChatID")
	if err != nil {
		return nil, nil, nil, err
	}

	return deliveryContextTo, directSessionPeerChatID, inboundSenderChatID, nil
}

func int64PtrFromField(structValue reflect.Value, fieldName string) (*int64, error) {
	field := structValue.FieldByName(fieldName)
	if !field.IsValid() {
		return nil, nil
	}
	if field.Kind() != reflect.Pointer {
		return nil, fmt.Errorf("runtime context field %s must be *int64", fieldName)
	}
	if field.IsNil() {
		return nil, nil
	}
	elem := field.Elem()
	if elem.Kind() != reflect.Int64 {
		return nil, fmt.Errorf("runtime context field %s must be *int64", fieldName)
	}
	value := elem.Int()
	result := int64(value)
	return &result, nil
}
