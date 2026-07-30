package apiutil

import (
	"encoding/json"
	"errors"
	"net/http"
)

type WebError struct {
	Code    int
	Message string
}

func (w *WebError) Error() string {
	return w.Message
}

func ServeError(w http.ResponseWriter, err error) {
	w.Header().Set("content-type", "application/json")
	if err, ok := errors.AsType[*WebError](err); ok {
		w.WriteHeader(err.Code)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
	encoded, _ := json.Marshal(map[string]string{"error": err.Error()})
	w.Write(encoded)
}

func ServeData(w http.ResponseWriter, obj any) {
	w.Header().Set("content-type", "application/json")
	encoded, _ := json.Marshal(map[string]any{"data": obj})
	w.Write(encoded)
}
