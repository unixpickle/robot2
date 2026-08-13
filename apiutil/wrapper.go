package apiutil

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func ServeAPI[T any, T1 any](w http.ResponseWriter, r *http.Request, f func(T) (T1, error)) {
	var obj T
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		ServeError(w, &WebError{
			Message: fmt.Sprintf("invalid JSON in request: %s", err),
			Code:    http.StatusBadRequest,
		})
		return
	}
	if resp, err := f(obj); err != nil {
		ServeError(w, err)
	} else {
		ServeData(w, resp)
	}
}
