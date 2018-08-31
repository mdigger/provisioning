package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"

	bolt "go.etcd.io/bbolt"
	gmail "google.golang.org/api/gmail/v1"
)

// MailTemplate описывает шаблон для формирования почтового сообщения.
type MailTemplate struct {
	Subject  string `json:"subject"`
	Template string `json:"template"`
	HTML     bool   `json:"html,omitempty"`
	template *template.Template
}

// Send отсылает почтовое уведомление на указанный адрес с использованием
// шаблона.
func (mt *MailTemplate) Send(client *gmail.Service,
	to string, data interface{}) error {
	var buf = new(bytes.Buffer)
	fmt.Fprintf(buf, "MIME-Version: %s\r\n", "1.0")
	fmt.Fprintf(buf, "To: %s\r\n", to)
	if mt.Subject != "" {
		fmt.Fprintf(buf, "Subject: %s\r\n",
			mime.QEncoding.Encode("utf-8", mt.Subject))
	}
	buf.WriteString("Content-Type: ")
	if mt.HTML {
		buf.WriteString("text/html\r\n")
	} else {
		buf.WriteString("text/plain\r\n")
	}
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	var enc = quotedprintable.NewWriter(buf)
	if err := mt.template.Execute(enc, data); err != nil {
		return err
	}
	enc.Close()
	_, err := client.Users.Messages.Send("me", &gmail.Message{
		Raw: base64.RawURLEncoding.EncodeToString(buf.Bytes()),
	}).Do()
	return err
}

// Template возвращает шаблон с указанным именем из хранилища.
func (s *Store) Template(name string) (*MailTemplate, error) {
	var config = new(MailTemplate)
	if err := s.db.View(func(tx *bolt.Tx) error {
		var bucket = tx.Bucket([]byte(sectionTemplates))
		if bucket == nil {
			return errors.New("email templates is not configured")
		}
		var data = bucket.Get([]byte(name))
		if data == nil {
			return fmt.Errorf("email template %s is not configured", name)
		}
		return json.Unmarshal(data, config)
	}); err != nil {
		return nil, err
	}

	var err error
	config.template, err = template.New("").Parse(config.Template)
	if err != nil {
		return nil, fmt.Errorf("email template error: %s", err)
	}
	return config, nil
}
