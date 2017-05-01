package main

import (
	"encoding/json"

	"github.com/boltdb/bolt"
	"github.com/mdigger/rest"
)

type Users struct {
	store *Store
}

// Get возвращает объединенный конфиг пользователя.
func (s *Users) Get(c *rest.Context) error {
	var user rest.JSON
	err := s.store.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("users"))
		if bucket == nil {
			return rest.ErrNotFound
		}
		// получаем и декодируем описание пользователя
		value := bucket.Get([]byte(c.Param("name")))
		user = make(rest.JSON)
		err := json.Unmarshal(value, &user)
		if err != nil {
			return err
		}

		// проверяем, что у пользователя определена группа
		group, ok := user["@group"]
		if !ok {
			return nil
		}
		// проверяем, что она задана строкой
		groupname, ok := group.(string)
		if !ok {
			return nil
		}
		// удаляем ключ группы
		delete(user, "@group")
		// получаем информацию о группе
		bucket = tx.Bucket([]byte("groups"))
		if bucket == nil {
			return nil
		}
		// получаем и декодируем описание группы
		value = bucket.Get([]byte(groupname))
		if value == nil {
			return nil
		}
		params := make(rest.JSON)
		err = json.Unmarshal(value, &params)
		if err != nil {
			return err
		}
		// добавляем список сервисов группы в качестве параметров пользователя
		user["@params"] = params

		// получаем информацию о настройках сервисов
		bucket = tx.Bucket([]byte("services"))
		if bucket == nil {
			return nil
		}
		// перебираем все сервисы группы поименно
		services := make(map[string]json.RawMessage, len(params))
		for name := range params {
			// получаем  описание сервиса
			value = bucket.Get([]byte(name))
			if value == nil {
				continue
			}
			services[name] = json.RawMessage(value)
		}
		user["@services"] = services
		return nil
	})
	if err != nil {
		return err
	}
	return c.Write(user)
}
