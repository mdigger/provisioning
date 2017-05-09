package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/mdigger/rest"
)

// Encoder используется для формирования ответов в формате JSON. Подменяет
// стандартный сериализатор от библиотеки rest.
func Encoder(c *rest.Context, v interface{}) error {
	// инициализируем формат и отдачу результата
	c.SetContentType("application/json; charset=utf-8")
	enc := json.NewEncoder(c.Response)
	enc.SetIndent("", "    ")
	// обрабатываем ошибки специальным образом
	if err, ok := v.(error); ok {
		return enc.Encode(&struct {
			Error string `json:"error,omitempty"`
		}{
			Error: err.Error(), // описание ошибки
		})
	}
	return enc.Encode(v) // сериализуем ответ в формат JSON
}

// ValidateEmail проверяет соответствие указанной в параметре строки на формат
// описания email адреса.
func ValidateEmail(email string) bool {
	var emailRegexp = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")
	return emailRegexp.MatchString(email)
}

func passwordGenerator() string {
	const dictionary = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, 12)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = dictionary[b%byte(len(dictionary))]
	}
	return fmt.Sprintf("%s-%s-%s-%s", bytes[:3], bytes[3:6], bytes[6:9], bytes[9:12])
}
