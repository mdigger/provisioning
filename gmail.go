package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/http"

	"github.com/boltdb/bolt"
	"github.com/mdigger/rest"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmail "google.golang.org/api/gmail/v1"
)

// GmailConfig описывает данные для авторизации gmail.
type GmailConfig struct {
	ID     string        `json:"client_id"`
	Secret string        `json:"client_secret"`
	Token  *oauth2.Token `json:"token"`
}

// Config возвращает конфигурацию для авторизации OAuth2.
func (gc *GmailConfig) Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     gc.ID,
		ClientSecret: gc.Secret,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}
}

// Client возвращает инициализированный клиент Gmail.
func (gc *GmailConfig) Client() (*gmail.Service, error) {
	return gmail.New(gc.Config().Client(context.Background(), gc.Token))
}

// MailTemplate описывает шаблон для формирования почтового сообщения.
type MailTemplate struct {
	Subject  string `json:"subject"`
	Template string `json:"template"`
	HTML     bool   `json:"html,omitempty"`
}

// Send отправляет почтовое сообщение
func (s *Store) Send(to, token string) error {
	// инициализируем сервис gmail, если он не инициализирован
	s.mu.RLock()
	mailTemplate := s.template
	gmailClient := s.gmail
	s.mu.RUnlock()
	// проверяем, что почтовый шаблон инициализирован
	if mailTemplate == nil {
		var config = new(MailTemplate)
		if err := s.db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte(bucketConfig))
			if bucket == nil {
				return errors.New("email template is not configured")
			}
			data := bucket.Get([]byte("template"))
			if data == nil {
				return errors.New("email template is not configured")
			}
			return json.Unmarshal(data, config)
		}); err != nil {
			return err
		}
		if config.Template == "" {
			config.Template = "%s"
		}
		// сохраняем шаблон в глобальном объекте для быстрого доступа
		mailTemplate = config
		s.mu.Lock()
		s.template = mailTemplate
		s.mu.Unlock()
	}
	// проверяем, что почтовый клиент инициализирован
	if gmailClient == nil {
		// получаем конфигурацию для gmail
		var gmailConfig *GmailConfig
		err := s.db.View(func(tx *bolt.Tx) error {
			// инициализируем доступ к разделу с данными администратора
			bucket := tx.Bucket([]byte(bucketConfig))
			if bucket == nil {
				return errors.New("email is not configured")
			}
			data := bucket.Get([]byte("gmail"))
			if data == nil {
				return errors.New("email is not configured")
			}
			gmailConfig = new(GmailConfig)
			return json.Unmarshal(data, gmailConfig)
		})
		if err != nil {
			return err
		}
		// инициализируем клиента
		gmailClient, err = gmailConfig.Client()
		if err != nil {
			return err
		}
		// сохраняем клиента для доступа
		s.mu.Lock()
		s.gmail = gmailClient
		s.mu.Unlock()
	}

	// формируем заголовок сообщения
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "MIME-Version: %s\r\n", "1.0")
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n",
		mime.QEncoding.Encode("utf-8", mailTemplate.Subject))
	buf.WriteString("Content-Type: ")
	if mailTemplate.HTML {
		buf.WriteString("text/html\r\n")
	} else {
		buf.WriteString("text/plain\r\n")
	}
	// после последнего заголовка двойной отступ
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	// кодируем тело сообщения
	enc := quotedprintable.NewWriter(&buf)
	_, err := io.WriteString(enc, fmt.Sprintf(mailTemplate.Template, token))
	if err != nil {
		return err
	}
	enc.Close()
	// отправляем почтовое сообщение через gmail
	_, err = gmailClient.Users.Messages.Send("me", &gmail.Message{
		Raw: base64.RawURLEncoding.EncodeToString(buf.Bytes()),
	}).Do()
	return err
}

// SetMailConfig сохраняет конфигурацию для доступа к gmail.
func (s *Store) SetMailConfig(c *rest.Context) error {
	// получаем идентификаторы для авторизации
	gcfg := new(struct {
		ID     string `json:"client_id" form:"client_id"`
		Secret string `json:"client_secret" form:"client_secret"`
		Code   string `json:"code"`
	})
	if err := c.Bind(gcfg); err != nil {
		return err
	}
	if gcfg.ID == "" {
		return c.Error(http.StatusBadRequest, "client_id required")
	}
	if gcfg.Secret == "" {
		return c.Error(http.StatusBadRequest, "client_secret required")
	}
	// конфигурация для авторизации
	cfg := &oauth2.Config{
		ClientID:     gcfg.ID,
		ClientSecret: gcfg.Secret,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}
	// если код для получения токена не задан, то отдаем URL для его получения
	if gcfg.Code == "" {
		return c.Write(cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline))
	}
	// запрашиваем токен для авторизации
	token, err := cfg.Exchange(oauth2.NoContext, gcfg.Code)
	if err != nil {
		return err
	}
	// сохраняем полученный токен в хранилище
	gmailConfig := &GmailConfig{
		ID:     gcfg.ID,
		Secret: gcfg.Secret,
		Token:  token,
	}
	data, err := json.MarshalIndent(gmailConfig, "", "    ")
	if err != nil {
		return err
	}
	if err = s.db.Update(func(tx *bolt.Tx) error {
		// инициализируем раздел, если он не был создан ранее
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucketConfig))
		if err != nil {
			return err
		}
		// сохраняем данные о настройках доступа к почте в хранилище
		return bucket.Put([]byte("gmail"), data)
	}); err != nil {
		return err
	}
	// сбрасываем настройки клиента
	s.mu.Lock()
	s.gmail = nil
	s.mu.Unlock()
	return nil
}

// MailConfig отдает идентификаторы настройки доступа к gamil.
func (s *Store) MailConfig(c *rest.Context) error {
	// получаем идентификаторы для авторизации
	gcfg := new(struct {
		ID     string `json:"client_id"`
		Secret string `json:"client_secret"`
	})
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketConfig))
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

// MailTemplate отдает настройки шаблонов для отсылки почты.
func (s *Store) MailTemplate(c *rest.Context) error {
	// получаем идентификаторы для авторизации
	gcfg := new(MailTemplate)
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketConfig))
		if bucket == nil {
			return nil
		}
		data := bucket.Get([]byte("template"))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, gcfg)
	}); err != nil {
		return err
	}
	if gcfg.Template == "" {
		gcfg.Template = "%s"
	}
	return c.Write(gcfg)
}

// StoreTemplate сохраняет настройки шаблонов отправки почты.
func (s *Store) StoreTemplate(c *rest.Context) error {
	gcfg := new(MailTemplate)
	if err := c.Bind(gcfg); err != nil {
		return err
	}
	if gcfg.Template == "" {
		gcfg.Template = "%s"
	}
	data, err := json.MarshalIndent(gcfg, "", "    ")
	if err != nil {
		return err
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucketConfig))
		if err != nil {
			return err
		}
		return bucket.Put([]byte("template"), data)
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.template = gcfg
	s.mu.Unlock()
	return nil
}

func (s *Store) testMail(c *rest.Context) error {
	return s.Send("sedykh@gmail.com", "aabb010203040506070809aabb-01")
}
