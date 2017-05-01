package main

import (
	"encoding/json"

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
