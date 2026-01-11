package helper

import (
	"encoding/json"
	"net/http"
)

// Gunakan domain model Anda, tapi di sini kita definisikan ulang untuk kejelasan
// Anda bisa import internal/domain dan gunakan struct tersebut
type SuccessResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// respondJSON adalah helper internal untuk menulis JSON
func respondJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if payload != nil {
		json.NewEncoder(w).Encode(payload)
	}
}

// respondSuccess adalah cara standar untuk mengirim respons sukses
func RespondSuccess(w http.ResponseWriter, code int, message string, data interface{}) {
	if message == "" {
		message = "Success"
	}
	payload := SuccessResponse{
		Success: true,
		Message: message,
		Data:    data,
	}
	respondJSON(w, code, payload)
}

// respondError adalah cara standar untuk mengirim respons error
func RespondError(w http.ResponseWriter, code int, message string, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	payload := ErrorResponse{
		Success: false,
		Message: message,
		Error:   errMsg,
	}
	respondJSON(w, code, payload)
}
