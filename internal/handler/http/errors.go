package handler

import "errors"

var (
	errInvalidRequestBody   = errors.New("invalid request body")
	errLoginTaken           = errors.New("login already taken")
	errInvalidCredentials   = errors.New("invalid login or password")
	errInvalidOrderNumber   = errors.New("invalid order number")
	errInsufficientFunds    = errors.New("insufficient funds")
	errOrderAlreadyUploaded = errors.New("order already uploaded by another user")
	errInternal             = errors.New("internal server error")
)
