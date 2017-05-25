package main

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Password описывает строку с паролем. При сохранении в хранилище она
// всегда преобразуется в bcrypt-hash.
type Password string

// MarshalText преобразует строку с паролем в bcrypt-hash, если это не было
// сделано до этого. В противном случае строка остается в неизменном виде.
func (p Password) MarshalText() ([]byte, error) {
	data := []byte(p)
	if _, err := bcrypt.Cost(data); err == nil {
		return data, nil
	}
	return bcrypt.GenerateFromPassword(data, bcrypt.DefaultCost)
}

// Compare возвращает true, если пароль совпадает с указанным в параметре.
func (p Password) Compare(password string) bool {
	data := []byte(p)
	// если пароль не хеширован, то просто сравниваем строки
	if _, err := bcrypt.Cost(data); err != nil {
		return string(p) == password
	}
	return bcrypt.CompareHashAndPassword(data, []byte(password)) == nil
}

func NewPassword() Password {
	const dictionary = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, 12)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = dictionary[b%byte(len(dictionary))]
	}
	return Password(fmt.Sprintf("%s-%s-%s-%s",
		bytes[:3], bytes[3:6], bytes[6:9], bytes[9:12]))
}
