package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/mdigger/rest"
	bolt "go.etcd.io/bbolt"
)

// Store описывает хранилище с информацией.
type Store struct {
	db *bolt.DB // хранилище данных
}

// OpenStore открывает хранилище данных.
func OpenStore(filename string) (*Store, error) {
	db, err := bolt.Open(filename, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close закрывает хранилище данных.
func (s *Store) Close() error {
	return s.db.Close()
}

// Название разделов хранилища с разной информацией.
const (
	sectionGroups    = "groups"
	sectionServices  = "services"
	sectionUsers     = "users"
	sectionUserData  = "data"
	sectionAdmins    = "admins"
	sectionConfig    = "config"
	sectionTemplates = "templates"
	sectionReset     = "reset"
)

// List отдает JSON со списком ключей в указанном разделе хранилища.
// Возвращает rest.ErrNotFound, если раздел в хранилище не найден.
func (s *Store) List(section string) rest.Handler {
	return func(c *rest.Context) error {
		var list []string
		if err := s.db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte(section))
			if bucket == nil {
				return c.Error(http.StatusNotFound, "section not found")
			}
			list = make([]string, 0, bucket.Stats().KeyN)
			return bucket.ForEach(func(k, _ []byte) error {
				list = append(list, string(k))
				return nil
			})
		}); err != nil {
			return err
		}
		return c.Write(rest.JSON{section: list})
	}
}

// Item отдает содержимое именованной записи в соответствующем разделе
// хранилища. Имя элемента из запроса указывается вторым параметром.
// Если содержимое начинается с символа `{`, то считается, что это формат JSON.
// В противном случае отдается как строка. Если указанный раздел или ключ в
// хранилище не зарегистрировано, то возвращается rest.ErrNotFound.
func (s *Store) Item(section string) rest.Handler {
	return func(c *rest.Context) error {
		return s.db.View(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte(section))
			if bucket == nil {
				return c.Error(http.StatusNotFound, "section not found")
			}
			name := c.Param("name")
			data := bucket.Get([]byte(name))
			if data == nil {
				return c.Error(http.StatusNotFound, "item not found")
			}
			// если это объект, то отдаем его как JSON
			if len(data) > 1 && data[0] == '{' {
				return c.Write(json.RawMessage(data))
			}
			// для административного раздела отдаем пароли в виде JSON
			if section == sectionAdmins {
				return c.Write(rest.JSON{"password": string(data)})
			}
			// иначе — как строку
			return c.Write(data)
		})
	}
}

// Remove удаляет элемент с именем, указанным в запросе, из соответствующего
// раздела хранилища. Если раздел или ключ не найдены в хранилище, то
// возвращается ошибка rest.ErrNotFound.
func (s *Store) Remove(section string) rest.Handler {
	return func(c *rest.Context) error {
		return s.db.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte(section))
			if bucket == nil {
				return c.Error(http.StatusNotFound, "section not found")
			}
			name := c.Param("name")
			if bucket.Get([]byte(name)) == nil {
				return c.Error(http.StatusNotFound, "item not found")
			}
			return bucket.Delete([]byte(name))
		})
	}
}

// save сохраняет данные в указанном разделе хранилища с указанным именем
func (s *Store) save(section, name string, obj interface{}) error {
	var data []byte
	switch obj := obj.(type) {
	case string:
		data = []byte(obj)
	case []byte:
		data = obj
	case json.RawMessage:
		data = obj
	case Password: // хешируем пароль, если он уже не представлен в виде хеша
		var err error
		data, err = obj.MarshalText()
		if err != nil {
			return err
		}
	default:
		var err error
		data, err = json.MarshalIndent(obj, "", "    ")
		if err != nil {
			return err
		}
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(section))
		if err != nil {
			return err
		}
		return bucket.Put([]byte(name), data)
	})
}

// Update обновляет именованные данные в указанном разделе. В зависимости от
// раздела, поддерживается разная обработка входящих данных в запросе.
func (s *Store) Update(section string) rest.Handler {
	return func(c *rest.Context) error {
		name := c.Param("name") // получаем имя ключа
		var obj interface{}     // объект для сохранения
		// в зависимости от раздела, разбираем входящие данные
		switch section {
		default: // обрабатываем любые данные как JSON
			var data = make(rest.JSON)
			if err := c.Bind(&data); err != nil {
				return c.Error(http.StatusBadRequest, err.Error())
			}
			obj = data
		case sectionUsers: // пользователь
			// проверяем, что это похоже на email
			if !strings.ContainsRune(name, '@') {
				return c.Error(http.StatusBadRequest, "bad user email")
			}
			var user = new(User)
			if err := c.Bind(user); err != nil {
				return c.Error(http.StatusBadRequest, err.Error())
			}
			if user.Group == "" {
				return c.Error(http.StatusBadRequest, "user group required")
			}
			if user.Password == "" {
				return c.Error(http.StatusBadRequest, "user password required")
			}
			user.Updated = time.Now().UTC()
			obj = user
		case sectionAdmins: // пароли администратора
			data := new(struct {
				Password `json:"password"`
			})
			if err := c.Bind(data); err != nil {
				return c.Error(http.StatusBadRequest, err.Error())
			}
			if data.Password == "" {
				return c.Error(http.StatusBadRequest, "password required")
			}
			obj = data.Password
		case sectionTemplates: // почтовый шаблон
			var data = new(MailTemplate)
			if err := c.Bind(data); err != nil {
				return c.Error(http.StatusBadRequest, err.Error())
			}
			// проверяем валидность шаблона
			if _, err := template.New("").Parse(data.Template); err != nil {
				return c.Error(http.StatusBadRequest, fmt.Sprintf(
					"template error: %s", err))
			}
			obj = data
		}
		return s.save(section, name, obj)
	}
}

// AdminAuth проверяет авторизацию администратора сервиса, если она задана.
func (s *Store) AdminAuth(c *rest.Context) error {
	username, password, ok := c.BasicAuth()
	return s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(sectionAdmins))
		// если раздел не задан или в нем нет ни одной записи,
		// то авторизация не требуется
		if bucket == nil || bucket.Stats().KeyN == 0 {
			return nil
		}
		if !ok {
			realm := fmt.Sprintf("Basic realm=%s admin", appName)
			c.SetHeader("WWW-Authenticate", realm)
			return rest.ErrUnauthorized
		}
		c.AddLogField("admin", username) // добавляем в лог имя администратора
		data := bucket.Get([]byte(username))
		if data == nil {
			return c.Error(http.StatusForbidden, "bad admin name")
		}
		if !(Password(data).Compare(password)) {
			return c.Error(http.StatusForbidden, "bad admin password")
		}
		return nil
	})
}

// Backup отдает представление хранилища в виде одного большого JSON пакета.
func (s *Store) Backup(c *rest.Context) error {
	result := make(rest.JSON) // результирующий JSON
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			section := make(rest.JSON)
			if err := b.ForEach(func(k, v []byte) error {
				name := string(k) // ключ
				if v == nil {
					section[name] = nil
				} else if len(v) > 1 && v[0] == '{' {
					// если это похоже на JSON, то считаем что это JSON
					section[name] = json.RawMessage(v)
				} else {
					// в противном случае считаем, что это строка
					section[name] = string(v)
				}
				return nil
			}); err != nil {
				return err
			}
			result[string(name)] = section
			return nil
		})
	}); err != nil {
		return err
	}
	return c.Write(result)
}
