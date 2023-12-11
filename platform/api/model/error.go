package model

import "errors"

// define global stackerr
var (
	ErrConflict = errors.New("conflict")
	ErrNotFound = errors.New("not found")
)
