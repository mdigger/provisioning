package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/mdigger/log"
	"github.com/mdigger/rest"
	"golang.org/x/crypto/bcrypt"
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
	template *template.Template
}

// Send отправляет почтовое сообщение
func (s *Store) Send(to, token, name string) error {
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
			config.Template = "{{.token}}"
		}
		// компилируем шаблон
		t, err := template.New("").Parse(config.Template)
		if err != nil {
			return err
		}
		config.template = t
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
	if mailTemplate.template == nil {
		log.Debug("bad compiled template")
	}
	enc := quotedprintable.NewWriter(&buf)
	if err := mailTemplate.template.Execute(enc, rest.JSON{
		"email": to,
		"token": token,
		"name":  name,
	}); err != nil {
		return err
	}
	enc.Close()
	// отправляем почтовое сообщение через gmail
	_, err := gmailClient.Users.Messages.Send("me", &gmail.Message{
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
	return c.Write(gcfg)
}

// StoreTemplate сохраняет настройки шаблонов отправки почты.
func (s *Store) StoreTemplate(c *rest.Context) error {
	gcfg := new(MailTemplate)
	err := c.Bind(gcfg)
	if err != nil {
		return err
	}
	// проверяем валидность шаблона
	gcfg.template, err = template.New("").Parse(gcfg.Template)
	if err != nil {
		return err
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

// ResetData описывает данные для сброса пароля
type ResetData struct {
	Code string    `json:"code"`
	Date time.Time `json:"created"`
}

// SendPassword отправляет почтовое уведомление о сбросе пароля.
func (s *Store) SendPassword(c *rest.Context) error {
	email := c.Param("name") // имя пользователя
	// проверяем, что указанное имя является email
	if !ValidateEmail(email) {
		return rest.ErrNotFound
	}
	// проверяем, что такой пользователь зарегистрирован
	user := new(User)
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketUsers))
		if bucket == nil {
			return rest.ErrNotFound
		}
		data := bucket.Get([]byte(email))
		if data == nil {
			return rest.ErrNotFound
		}
		return json.Unmarshal(data, user)
	}); err != nil {
		return err
	}
	// формируем данные для сброса пароля
	reset := &ResetData{
		Code: passwordGenerator(),
		Date: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(reset, "", "    ")
	if err != nil {
		return err
	}
	// сохраняем в хранилище
	if err := s.db.Update(func(tx *bolt.Tx) error {
		// инициализируем раздел, если он не был создан ранее
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucketReset))
		if err != nil {
			return err
		}
		// сохраняем данные о сервисе в хранилище
		return bucket.Put([]byte(email), data)
	}); err != nil {
		return err
	}
	// формируем токен
	token := base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf("%s:%s", email, reset.Code)))
	// отправляем его почтой
	return s.Send(email, token, user.Name)
}

var ValidTokenPeriod = time.Hour * 24 * 5

// ResetPassword заменяет пароль пользователя
func (s *Store) ResetPassword(c *rest.Context) error {
	// декодируем токен для сброса пароля
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
	// создаем новый пароль
	password := passwordGenerator()
	// проверяем код и заменяем пароль пользователя
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketReset))
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
		if reset.Code != code {
			return c.Error(http.StatusNotFound, "bad token")
		}
		if reset.Date.After(time.Now().Add(ValidTokenPeriod)) {
			return c.Error(http.StatusNotFound, "token expired")
		}
		// обращаемся к данным пользователя
		bucket = tx.Bucket([]byte(bucketUsers))
		if bucket == nil {
			return c.Error(http.StatusNotFound, "token user not found")
		}
		// получаем данные о пользователе
		data = bucket.Get([]byte(name))
		if data == nil {
			return c.Error(http.StatusNotFound, "token user not found")
		}
		// декодируем данные пользователя
		user := new(User)
		if err := json.Unmarshal(data, user); err != nil {
			return err
		}
		// хешируем новый пароль и сохраняем его у пользователя
		data, err = bcrypt.GenerateFromPassword(
			[]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		user.Password = string(data)
		// кодируем данные пользователя в формат JSON
		data, err = json.MarshalIndent(user, "", "    ")
		if err != nil {
			return err
		}
		// сохраняем новые данные пользователя в хранилище
		return bucket.Put([]byte(name), data)
	}); err != nil {
		return err
	}
	// возвращаем новый пароль
	return c.Write(rest.JSON{"password": password})
}
