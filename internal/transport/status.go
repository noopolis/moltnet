package transport

import (
	"errors"
	"net/http"
)

type statusCoder interface {
	StatusCode() int
}

func statusForError(err error) int {
	var coder statusCoder
	if errors.As(err, &coder) {
		return coder.StatusCode()
	}

	switch {
	case err == nil:
		return http.StatusOK
	default:
		return http.StatusInternalServerError
	}
}
