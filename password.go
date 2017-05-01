package main

import "crypto/rand"

const PasswordSection = "@passwords"

// GeneratePassword возвращает случайный сгенерированный пароль.
func generatePassword() []byte {
	const passwordDictionary = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, 12) // задаем размер пароля
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	for k, v := range bytes {
		bytes[k] = passwordDictionary[v%byte(len(passwordDictionary))]
	}
	return bytes
}
