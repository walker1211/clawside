package main

import "errors"

type serviceError string

func (e serviceError) Error() string {
	return string(e)
}

var (
	ErrBotRequired        = serviceError("bot is required")
	ErrUnknownBot         = serviceError("unknown bot")
	ErrBotDisabled        = serviceError("bot is disabled")
	ErrBotUnavailable     = serviceError("bot is unavailable")
	ErrChatIDRequired     = serviceError("chat_id is required")
	ErrChatIDNotAllowed   = serviceError("chat_id is not allowed")
	ErrTextRequired       = serviceError("text is required")
	ErrTextExceedsLimit   = serviceError("text exceeds telegram limit")
	ErrMaxAttemptsInvalid = serviceError("max_attempts must be between 1 and 5")
)

func sendErrorResponse(err error) (int, errorResponse) {
	switch {
	case errors.Is(err, ErrBotRequired),
		errors.Is(err, ErrUnknownBot),
		errors.Is(err, ErrChatIDRequired),
		errors.Is(err, ErrTextRequired),
		errors.Is(err, ErrTextExceedsLimit),
		errors.Is(err, ErrMaxAttemptsInvalid):
		return 400, errorResponse{Error: err.Error()}
	case errors.Is(err, ErrBotDisabled),
		errors.Is(err, ErrBotUnavailable),
		errors.Is(err, ErrChatIDNotAllowed):
		return 403, errorResponse{Error: err.Error()}
	default:
		return 500, errorResponse{Error: "failed to enqueue job"}
	}
}
