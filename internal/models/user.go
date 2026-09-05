package models

// User представляет зарегистрированного пользователя системы
type User struct {
	ID       int64
	Login    string
	Password string // bcrypt-хеш
}

// LoginRequest представляет JSON-тело запроса регистрации и аутентификации
type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}
