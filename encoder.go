package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"regexp"
	"strings"

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

// Bind декодирует переданные в запросе данные в объект.
func Bind(r *http.Request, obj interface{}) error {
	// проверяем тип запроса и данных
	if r.Method != "POST" && r.Method != "PUT" {
		return rest.ErrUnsupportedHTTPMethod
	}
	mediatype, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediatype != "application/json" {
		return rest.ErrUnsupportedContentType
	}
	charset, ok := params["charset"]
	if ok && strings.ToUpper(charset) != "UTF-8" {
		return rest.ErrUnsupportedCharset
	}
	// декодируем данные запроса в объект
	return json.NewDecoder(r.Body).Decode(obj)
}

var emailRegexp = regexp.MustCompile("^[a-zA-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$")

// ValidateEmail проверяет соответствие указанной в параметре строки на формат
// описания email адреса.
func ValidateEmail(email string) bool {
	return emailRegexp.MatchString(email)
}
