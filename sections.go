package main

import (
	"encoding/json"

	"github.com/mdigger/rest"
)

// Sections поддерживает работу с разделами конфигурации через HTTP.
type Sections struct {
	store *Store
}

// List возвращает список ключей, определенных в хранилище для данной секции
func (s *Sections) List(c *rest.Context) error {
	section := c.Param("section")
	list, err := s.store.Keys(section)
	if err != nil {
		return err
	}
	return c.Write(rest.JSON{section: list})
}

// Get отдает ответ со значением указанного ключа. Отдает http NotFound, если
// ни данные для запрошенного ключа не найдены.
func (s *Sections) Get(c *rest.Context) error {
	section, key := c.Param("section"), c.Param("key")
	data, err := s.store.Get(section, key)
	if err != nil {
		return err
	}
	if data == nil || data[key] == nil {
		return rest.ErrNotFound
	}
	return c.Write(data[key])
}

// Post сохраняет описание сервисов в указанном разделе хранилища. Пустые
// описания сервисов при этом автоматически удаляются.
func (s *Sections) Post(c *rest.Context) error {
	obj := make(rest.JSON)
	if err := json.NewDecoder(c.Request.Body).Decode(&obj); err != nil {
		return err
	}
	return s.store.Save(c.Param("section"), obj)
}

// Put добавляет новое описание или изменяет уже существующее сервиса.
func (s *Sections) Put(c *rest.Context) error {
	obj := make(rest.JSON)
	if err := json.NewDecoder(c.Request.Body).Decode(&obj); err != nil {
		return err
	}
	return s.store.Save(c.Param("section"), rest.JSON{c.Param("key"): obj})
}

// Delete удаляет описание из секции для указанного ключа. Если такого ключа
// нет, ошибка все равно не возвращается.
func (s *Sections) Delete(c *rest.Context) error {
	return s.store.Remove(c.Param("section"), c.Param("key"))
}
