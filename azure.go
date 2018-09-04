package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	app "github.com/mdigger/app-info"
	"github.com/mdigger/jwt"
	"github.com/mdigger/rest"
)

var (
	jwksURL    = "https://login.microsoftonline.com/common/discovery/keys"
	httpClient = &http.Client{Timeout: time.Second * 10}
	authKeys   = new(Auth)
)

// Auth отвечате за авторизацию и проверку токенов.
type Auth struct {
	jwkeys map[string]interface{} // список ключей
	mu     sync.RWMutex
}

// GetKey возвращает ключ для проверки подписи по его идентификатору. Если
// необходимо, то происходит загрузка ключей.
func (a *Auth) GetKey(_, keyID string) interface{} {
	authKeys.mu.Lock()
	defer authKeys.mu.Unlock()
	// возвращаем ключ, если он существует
	if authKeys.jwkeys != nil {
		if key, ok := authKeys.jwkeys[keyID]; ok {
			return key
		}
	}
	// ключ не найден — получаем актуальный список ключей
	keys, err := GetJWKeys(jwksURL)
	if err != nil {
		return err
	}
	authKeys.jwkeys = keys // сохраняем список ключей
	return keys[keyID]
}

// GetJWKeys запрашивает и разбирает список ключей.
func GetJWKeys(url string) (map[string]interface{}, error) {
	// формируем запрос для получения ключей
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", app.Agent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, rest.ErrServiceUnavailable // сервер провайдера ответил ошибкой
	}
	// разбираем список полученных ключей
	var jwkeys = new(struct {
		Keys []*jwt.JWK `json:"keys"` // список ключей
	})
	err = json.NewDecoder(resp.Body).Decode(&jwkeys)
	if err != nil {
		return nil, err
	}
	// разбираем ключи и преобразуем их в словарь по их идентификатору
	var result = make(map[string]interface{}, len(jwkeys.Keys))
	for _, key := range jwkeys.Keys {
		decodedKey, err := key.Decode()
		if err != nil {
			return nil, err
		}
		result[key.ID] = decodedKey
	}
	return result, nil
}
