package main

import (
	"encoding/json"
	"errors"

	"golang.org/x/crypto/bcrypt"

	"github.com/boltdb/bolt"
	"github.com/mdigger/rest"
)

const AdminSection = "@admins"

// Admins описывает раздел хранилища с паролями.
type Admins struct {
	store *Store
}

func (s *Admins) List(c *rest.Context) error {
	list, err := s.store.Keys(AdminSection)
	if err != nil {
		return err
	}
	return c.Write(rest.JSON{AdminSection: list})
}

// Get возвращает ошибку, если такой пользователь не зарегистрирован. Если
// зарегистрирован, то ошибки не возвращается.
func (s *Admins) Get(c *rest.Context) error {
	key := c.Param("key")
	data, err := s.store.Get(AdminSection, key)
	if err != nil {
		return err
	}
	if data == nil || data[key] == nil {
		return rest.ErrNotFound
	}
	return nil
}

// Post позволяет переопределить пароли сразу для нескольких пользователей
func (s *Admins) Post(c *rest.Context) error {
	obj := make(map[string]string)
	if err := json.NewDecoder(c.Request.Body).Decode(&obj); err != nil {
		return err
	}
	// заменяем пароли на их хеш
	passwords := make(map[string]interface{}, len(obj))
	for name, value := range obj {
		// пропускаем пустые пароли
		if len(value) == 0 {
			continue
		}
		// в противном случае сохраняем в хранилище хеш от пароля
		hashedPassword, err := bcrypt.GenerateFromPassword(
			[]byte(value), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		passwords[name] = hashedPassword
	}
	return s.store.Save(AdminSection, passwords)
}

// Put добавляет новое описание или изменяет уже существующее сервиса.
func (s *Admins) Put(c *rest.Context) error {
	var passwd = new(struct {
		Password string `json:"password"`
	})
	if err := json.NewDecoder(c.Request.Body).Decode(passwd); err != nil {
		return err
	}
	if passwd.Password == "" {
		return rest.ErrBadRequest
	}
	// в противном случае сохраняем в хранилище хеш от пароля
	hashedPassword, err := bcrypt.GenerateFromPassword(
		[]byte(passwd.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.store.Save(AdminSection, rest.JSON{c.Param("key"): hashedPassword})
}

// Delete удаляет описание из секции для указанного ключа. Если такого ключа
// нет, ошибка все равно не возвращается.
func (s *Admins) Delete(c *rest.Context) error {
	return s.store.Remove(AdminSection, c.Param("key"))
}

// Check проверяет авторизацию администратора.
func (s *Admins) Check(c *rest.Context) error {
	// запрашиваем информацию об авторизации запроса
	username, password, ok := c.BasicAuth()
	// обращаемся к базе данных с авторизацией
	var (
		hashedPassword []byte
		errNoAuth      = errors.New("no authorization required")
	)
	err := s.store.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(AdminSection))
		// проверяем, что секция авторизации существует
		// в противном случае подходят все логины и пароли
		// если в секции нет ни одной записи, то авторизация не требуется
		if bucket == nil || bucket.Stats().KeyN == 0 {
			return errNoAuth
		}
		// получаем сохраненный пользователем пароль
		hashedPassword = bucket.Get([]byte(username))
		return nil
	})
	// проверяем, что авторизация требуется
	if err == errNoAuth {
		// log.Warning("no admin authorization required")
		return nil
	}
	// проверяем другие ошибки доступа к хранилищу
	if err != nil {
		return err
	}
	// требуется авторизация
	if !ok {
		return rest.ErrUnauthorized
	}
	// если пароль пустой, значит такого пользователя нет в админах
	if hashedPassword == nil {
		return rest.ErrForbidden
	}
	// сравниваем полученный пароль с указанным пользователем
	err = bcrypt.CompareHashAndPassword(hashedPassword, []byte(password))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		return rest.ErrForbidden
	}
	// отдаем ошибку или, если ее нет, позволяем продолжить
	return err
}
