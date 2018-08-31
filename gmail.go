package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/mdigger/rest"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmail "google.golang.org/api/gmail/v1"
)

var (
	gmailService *gmail.Service // почтовый сервис
	mu           sync.RWMutex
)

// GmailConfig описывает конфигурацию для инициализации gmail.
type GmailConfig struct {
	ID     string        `json:"id"`
	Secret string        `json:"secret"`
	Token  *oauth2.Token `json:"token,omitempty"`
}

// GmailClient возвращает инициализированный клиент для отправки почтовых
// уведомлений.
func (s *Store) GmailClient() (*gmail.Service, error) {
	mu.RLock()
	service := gmailService
	mu.RUnlock()
	if service != nil {
		return service, nil // сервис уже инициализирован
	}
	gcfg := new(GmailConfig)
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionConfig))
		if bucket == nil {
			return nil
		}
		data := bucket.Get([]byte("gmail"))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, gcfg)
	}); err != nil {
		return nil, err
	}
	var config = &oauth2.Config{
		ClientID:     gcfg.ID,
		ClientSecret: gcfg.Secret,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}
	var client = config.Client(context.Background(), gcfg.Token)
	service, err := gmail.New(client)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	gmailService = service
	mu.Unlock()
	return service, nil
}

func (s *Store) Send(user *User, templateName string, data rest.JSON) error {
	client, err := s.GmailClient()
	if err != nil {
		return err
	}
	template, err := s.Template(templateName)
	if err != nil {
		return err
	}
	data["email"] = user.Email
	data["name"] = user.Name
	return template.Send(client, user.Email, data)
}

// SendWithTemplate отсылает письмо пользователю с помощью шаблона.
func (s *Store) SendWithTemplate(c *rest.Context) error {
	user, err := s.User(c.Param("to"))
	if err != nil {
		return err
	}
	data := make(rest.JSON)
	if err := c.Bind(&data); err != nil {
		return err
	}
	return s.Send(user, c.Param("name"), data)
}

// GetGmailConfig отдает настройки почты. Отдается только идентификатор и
// секретный ключ. Токен, полученный при авторизации, не отдается.
func (s *Store) GetGmailConfig(c *rest.Context) error {
	gcfg := new(struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	})
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionConfig))
		if bucket == nil {
			return nil
		}
		data := bucket.Get([]byte("gmail"))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, gcfg)
	}); err != nil {
		return err
	}
	return c.Write(gcfg)
}

// SetGmailConfig обрабатывает настройку почты.
func (s *Store) SetGmailConfig(c *rest.Context) error {
	gcfg := new(struct {
		ID     string `json:"id" form:"id"`
		Secret string `json:"secret"`
		Code   string `json:"code"`
	})
	if err := c.Bind(gcfg); err != nil {
		return c.Error(http.StatusBadRequest, err.Error())
	}
	if gcfg.ID == "" {
		return c.Error(http.StatusBadRequest, "id required")
	}
	if gcfg.Secret == "" {
		return c.Error(http.StatusBadRequest, "secret required")
	}
	cfg := &oauth2.Config{
		ClientID:     gcfg.ID,
		ClientSecret: gcfg.Secret,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}
	// если код для получения токена не задан, то отдаем URL для его получения
	if gcfg.Code == "" {
		// return c.Redirect(http.StatusTemporaryRedirect,
		// 	cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline))
		return c.Write(cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline))
	}

	token, err := cfg.Exchange(oauth2.NoContext, gcfg.Code)
	if err != nil {
		return err
	}
	if err := s.save(sectionConfig, "gmail", &GmailConfig{
		ID:     gcfg.ID,
		Secret: gcfg.Secret,
		Token:  token,
	}); err != nil {
		return err
	}
	service, err := gmail.New(cfg.Client(context.Background(), token))
	if err != nil {
		return err
	}
	mu.Lock()
	gmailService = service
	mu.Unlock()
	return nil
}
