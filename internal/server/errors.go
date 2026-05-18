package server

import (
	"encoding/json"
	"net/http"
)

// ErrorType represents the type of error.
type ErrorType string

const (
	SyntaxError      ErrorType = "SYNTAX_ERROR"
	SemanticError    ErrorType = "SEMANTIC_ERROR"
	TypeMismatch     ErrorType = "TYPE_MISMATCH"
	NotFound         ErrorType = "NOT_FOUND"
	GenericUserError ErrorType = "GENERIC_USER_ERROR"
)

// WrenError represents an application error.
type WrenError struct {
	Code    int
	Type    ErrorType
	Message string
	Cause   error
}

func (e *WrenError) Error() string {
	return e.Message
}

// ErrorMessageDto matches the Java ErrorMessageDto format.
type ErrorMessageDto struct {
	ErrorCode int       `json:"errorCode"`
	ErrorType ErrorType `json:"errorType"`
	Message   string    `json:"message"`
}

// WriteError writes an error response in the Java-compatible format.
func WriteError(w http.ResponseWriter, err *WrenError) {
	status := http.StatusInternalServerError
	switch err.Type {
	case SyntaxError, SemanticError, TypeMismatch:
		status = http.StatusBadRequest
	case NotFound:
		status = http.StatusNotFound
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorMessageDto{
		ErrorCode: err.Code,
		ErrorType: err.Type,
		Message:   err.Message,
	})
}
