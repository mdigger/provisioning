package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/mdigger/rest"
)

// User описывает структуру данных пользователя.
type User struct {
	Email    string               `json:"-"`                  // email адрес
	Name     string               `json:"name,omitempty"`     // имя пользователя
	Password Password             `json:"password"`           // хеш пароля пользователя
	Group    string               `json:"group"`              // название группы
	Services map[string]rest.JSON `json:"services,omitempty"` // параметры сервисов
	Updated  time.Time            `json:"updated"`            // время обновления
}

// User возвращает информацию о пользователе с указанным идентификатором.
func (s *Store) User(username string) (*User, error) {
	var user *User
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionUsers))
		if bucket == nil {
			return rest.ErrNotFound
		}
		data := bucket.Get([]byte(username))
		if data == nil {
			return rest.ErrNotFound
		}
		user = new(User)
		return json.Unmarshal(data, user)
	}); err != nil {
		return nil, err
	}
	user.Email = username
	return user, nil
}

// AuthUser возвращает информацию об авторизованном пользователе.
func (s *Store) AuthUser(c *rest.Context) (*User, error) {
	username, password, ok := c.BasicAuth()
	if !ok {
		realm := fmt.Sprintf("Basic realm=%s", appName)
		c.SetHeader("WWW-Authenticate", realm)
		return nil, rest.ErrUnauthorized
	}
	c.AddLogField("user", username) // добавляем в лог имя пользователя
	user, err := s.User(username)
	if err == rest.ErrNotFound {
		return nil, rest.ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	if !user.Password.Compare(password) {
		return nil, rest.ErrForbidden
	}
	return user, nil
}

// config возвращает объединенный конфигурационный файл для указанного
// пользователя.
func (s *Store) config(user *User) (interface{}, error) {
	var result interface{} = user.Services
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionGroups))
		if bucket == nil {
			return nil
		}
		data := bucket.Get([]byte(user.Group))
		if data == nil {
			return nil
		}
		groupServices := make(map[string]rest.JSON)
		if err := json.Unmarshal(data, &groupServices); err != nil {
			return err
		}
		if len(groupServices) == 0 {
			return nil
		}

		bucket = tx.Bucket([]byte(sectionServices))
		if bucket == nil {
			return nil
		}
		config := make(map[string]rest.JSON)
		for name, groupData := range groupServices {
			data = bucket.Get([]byte(name))
			if data == nil {
				config[name] = groupData
				continue
			}
			service := make(rest.JSON)
			if err := json.Unmarshal(data, &service); err != nil {
				return err
			}

			for serviceName, data := range groupData {
				service[serviceName] = data
			}
			for serviceName, data := range user.Services[name] {
				service[serviceName] = data
			}
			config[name] = service
			delete(user.Services, name) // чтобы потом не дублировать
		}
		for serviceName, data := range user.Services {
			config[serviceName] = data
		}
		result = config
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// Config возвращает объединенный конфиг пользователя. При этом проверяется
// авторизация пользователя и имя пользователя берется из нее.
func (s *Store) Config(c *rest.Context) error {
	user, err := s.AuthUser(c)
	if err != nil {
		return err
	}
	config, err := s.config(user)
	if err != nil {
		return err
	}
	return c.Write(config)
}

// UserConfig возвращает объединенный конфиг пользователя. При этом авторизация
// пользователя не проверяется, а имя пользователя берется из запроса.
func (s *Store) UserConfig(c *rest.Context) error {
	user, err := s.User(c.Param("name"))
	if err != nil {
		return err
	}
	config, err := s.config(user)
	if err != nil {
		return err
	}
	return c.Write(config)
}

// SetUserPassword заменяет пароль пользователя на новый
func (s *Store) SetUserPassword(c *rest.Context) error {
	user, err := s.AuthUser(c)
	if err != nil {
		return err
	}
	data := new(struct {
		Password string `json:"password"`
	})
	if err := c.Bind(data); err != nil {
		return err
	}
	if data.Password == "" {
		return c.Error(http.StatusBadRequest, "password required")
	}
	if user.Password.Compare(data.Password) {
		return nil
	}
	user.Password = Password(data.Password)
	user.Updated = time.Now().UTC()
	return s.save(sectionUsers, user.Email, user)
}

// ResetData описывает данные для сброса пароля
type ResetData struct {
	Code Password  `json:"code"`
	Date time.Time `json:"created"`
}

// PasswordToken генерирует токен для изменения пароля пользователя.
func (s *Store) PasswordToken(c *rest.Context) error {
	user, err := s.User(c.Param("name"))
	if err != nil {
		return err
	}
	reset := &ResetData{
		Code: NewPassword(),
		Date: time.Now().UTC(),
	}
	if err := s.save(sectionReset, user.Email, reset); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%s:%s", user.Email, reset.Code)))
	return s.Send(user, "resetPassword", rest.JSON{"token": token})
}

var ValidTokenPeriod = time.Hour * 24 * 5

// ResetPassword устанавливает новый пароль пользователя.
func (s *Store) ResetPassword(c *rest.Context) error {
	token, err := base64.RawURLEncoding.DecodeString(c.Param("token"))
	if err != nil {
		return c.Error(http.StatusNotFound, "bad token")
	}
	stoken := string(token)
	sindex := strings.IndexByte(stoken, ':')
	if sindex < 0 {
		return c.Error(http.StatusNotFound, "bad token")
	}
	name, code := stoken[:sindex], stoken[sindex+1:]
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionReset))
		if bucket == nil {
			return c.Error(http.StatusNotFound, "bad token")
		}
		// получаем данные для сброса
		data := bucket.Get([]byte(name))
		if data == nil {
			return c.Error(http.StatusNotFound, "bad token")
		}
		// удаляем данные для сброса
		if err := bucket.Delete([]byte(name)); err != nil {
			return err
		}
		// декодируем данные для сброса пароля
		reset := new(ResetData)
		if err := json.Unmarshal(data, reset); err != nil {
			return err
		}
		// проверяем время жизни токена и его код
		if !reset.Code.Compare(code) {
			return c.Error(http.StatusNotFound, "bad token")
		}
		if reset.Date.After(time.Now().Add(ValidTokenPeriod)) {
			return c.Error(http.StatusNotFound, "token expired")
		}
		return nil
	}); err != nil {
		return err
	}

	user, err := s.User(name)
	if err != nil {
		return c.Error(http.StatusNotFound, "bad user token")
	}
	user.Password = NewPassword()
	user.Updated = time.Now().UTC()
	// если в запросе есть параметр email, то отправить почту
	if len(c.Request.URL.Query()["email"]) > 0 {
		if err := s.Send(user, "newPassword",
			rest.JSON{"password": user.Password}); err != nil {
			return err
		}
	}
	if err := s.save(sectionUsers, name, user); err != nil {
		return err
	}
	return c.Write(rest.JSON{"password": string(user.Password)})
}

func (s *Store) UserDataPatch(c *rest.Context) error {
	var patchData = make(rest.JSON)
	if err := c.Bind(&patchData); err != nil {
		return err
	}
	name := c.Param("name")
	var userData = make(rest.JSON)
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionUserData))
		if bucket == nil {
			return nil
		}
		data := bucket.Get([]byte(name))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &userData)
	}); err != nil {
		return err
	}
	for k, v := range patchData {
		if v == nil {
			delete(userData, k)
		} else {
			userData[k] = v
		}
	}
	return s.save(sectionUserData, name, userData)
}

func (s *Store) UserData(c *rest.Context) error {
	user, err := s.AuthUser(c)
	if err != nil {
		return err
	}
	return s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionUserData))
		if bucket == nil {
			return c.Error(http.StatusNotFound, "section not found")
		}
		data := bucket.Get([]byte(user.Email))
		if data == nil {
			return c.Error(http.StatusNotFound, "user data item not found")
		}
		return c.Write(data)
	})
}
