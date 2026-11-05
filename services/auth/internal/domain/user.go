package domain

import "errors"

var (
	ErrInvalidInput       = errors.New("invalid credentials format")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailExists        = errors.New("email is already registered")
	ErrBusy               = errors.New("authentication capacity exceeded")
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
}
