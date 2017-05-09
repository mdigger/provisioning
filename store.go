package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/boltdb/bolt"
	"github.com/mdigger/rest"
	gmail "google.golang.org/api/gmail/v1"
)

// Store описывает хранилище информации для конфигурирования устройств.
type Store struct {
	db       *bolt.DB
	gmail    *gmail.Service
	template *MailTemplate
	mu       sync.RWMutex
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
	bucketGroups   = "groups"
	bucketServices = "services"
	bucketUsers    = "users"
	bucketAdmins   = "admins"
	bucketConfig   = "config"
)

// CheckAdmins проверяет авторизацию запроса администратора. Если ни одного
// администратора не задано, то авторизация не требуется. В противном случае,
// авторизация произойдет только в том случае, если администратор с таким
// логином задан и пароль совпадает.
func (s *Store) CheckAdmins(c *rest.Context) error {
	// получаем информацию об авторизации запроса
	username, password, ok := c.BasicAuth()
	return s.db.View(func(tx *bolt.Tx) error {
		// инициализируем доступ к разделу с данными администратора
		bucket := tx.Bucket([]byte(bucketAdmins))
		// если раздел администраторов не задан или в нем нет ни одной записи,
		// то авторизация не требуется
		if bucket == nil || bucket.Stats().KeyN == 0 {
			return nil // авторизация не требуется
		}
		// в противном случае требуется информация об авторизации в запросе
		if !ok {
			realm := fmt.Sprintf("Basic realm=%s admin", appName)
			c.SetHeader("WWW-Authenticate", realm)
			return rest.ErrUnauthorized
		}
		// запрашиваем пароль администратора из хранилища
		data := bucket.Get([]byte(username))
		// проверяем, что такой администратор зарегистрирован
		if data == nil {
			return rest.ErrForbidden
		}
		// сравниваем полученный пароль с указанным
		err := bcrypt.CompareHashAndPassword(data, []byte(password))
		if err == bcrypt.ErrMismatchedHashAndPassword {
			return rest.ErrForbidden
		}
		return err
	})
}

// list отдает в ответ список ключей в указанном разделе хранилища.
func (s *Store) list(section string) ([]string, error) {
	var list []string // результирующий список ключей
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(section))
		if bucket == nil {
			// мне не нравится, когда отдается в JSON null вместо пустого списка
			// поэтому предпочитаю явно отдать пустой список
			list = []string{}
			return nil
		}
		// инициализируем список ключей нужной длинны
		list = make([]string, 0, bucket.Stats().KeyN)
		// перебираем все ключи в хранилище
		return bucket.ForEach(func(name, _ []byte) error {
			list = append(list, string(name))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return list, nil
}

// List возвращает обработчик отдачи списка ключей для заданного раздела
// хранилища.
func (s *Store) List(section string) rest.Handler {
	return func(c *rest.Context) error {
		list, err := s.list(section)
		if err != nil {
			return err
		}
		return c.Write(rest.JSON{section: list})
	}
}

// get возвращает значение ключа из заданного раздела хранилища. Если такого
// ключа нет, то возвращается rest.ErrNotFound.
func (s *Store) get(section, name string) (rest.JSON, error) {
	var obj rest.JSON
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(section))
		if bucket == nil {
			return rest.ErrNotFound
		}
		data := bucket.Get([]byte(name))
		if data == nil {
			return rest.ErrNotFound
		}
		// не отдает информацию о паролях администраторов
		if section == bucketAdmins {
			obj = rest.JSON{"exists": true}
			return nil
		}
		obj = make(rest.JSON)
		return json.Unmarshal(data, &obj)
	})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// Get отдает конфигурацию объекта из раздела хранилища с заданным именем.
// Если объекта с таким именем в хранилище нет, то отдается rest.ErrNotFound.
// При запросе раздела администраторов всегда отдается rest.ErrForbidden.
func (s *Store) Get(section string) rest.Handler {
	return func(c *rest.Context) error {
		obj, err := s.get(section, c.Param("name"))
		if err != nil {
			return err
		}
		return c.Write(obj)
	}
}

// remove удаляет конфигурацию объекта с указанным именем из раздела хранилища.
// Если объект с таким именем в хранилище не зарегистрирован, то возвращается
// rest.ErrNotFound.
func (s *Store) remove(section, name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(section))
		if bucket == nil {
			return rest.ErrNotFound
		}
		if bucket.Get([]byte(name)) == nil {
			return rest.ErrNotFound
		}
		return bucket.Delete([]byte(name))
	})
}

// Remove возвращает обработчик удаления объектов из заданного раздела хранилища.
func (s *Store) Remove(section string) rest.Handler {
	return func(c *rest.Context) error {
		return s.remove(section, c.Param("name"))
	}
}

// save сохраняет сразу несколько именованных данных в указанном разделе. Имена
// данных при этом выступают ключами. Если какие-то из данных заданы пустым
// значением, то они будут удалены из хранилища.
func (s *Store) save(section string, objs map[string][]byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		// инициализируем раздел, если он не был создан ранее
		bucket, err := tx.CreateBucketIfNotExists([]byte(section))
		if err != nil {
			return err
		}
		// перебираем все данные
		for name, data := range objs {
			// если задано пустое описания, то он удаляем из хранилища
			if data == nil {
				if err := bucket.Delete([]byte(name)); err != nil {
					return err
				}
				continue
			}
			// сохраняем данные о сервисе в хранилище
			if err := bucket.Put([]byte(name), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// User описывает структуру данных пользователя.
type User struct {
	Name     string               `json:"name,omitempty"`     // имя пользователя
	Password string               `json:"password"`           // хеш пароля пользователя
	Group    string               `json:"group"`              // название группы
	Services map[string]rest.JSON `json:"services,omitempty"` // параметры сервисов
	Updated  time.Time            `json:"updated"`            // время обновления
}

// Post возвращает обработчик сохранения списка объектов в хранилище.
func (s *Store) Post(section string) rest.Handler {
	return func(c *rest.Context) error {
		var objs map[string][]byte
		// в зависимости от раздела, формат данных отличается
		switch section {
		case bucketAdmins:
			requestData := make(map[string]string)
			// производим непосредственный разбор данных
			if err := c.Bind(&requestData); err != nil {
				return err
			}
			// инициализируем результирующий список
			objs = make(map[string][]byte, len(requestData))
			// перебираем пароли админов и хешируем их, если нужно
			for name, password := range requestData {
				if password == "" {
					return c.Error(http.StatusBadRequest,
						fmt.Sprintf("admin %s password required", name))
				}
				// проверяем, что это хеш от пароля, а не сам пароль
				// если это не так, то хешируем пароль
				if _, err := bcrypt.Cost([]byte(password)); err == nil {
					// пароль уже представлен в виде хеша
					objs[name] = []byte(password)
					continue
				}
				// хешируем пароль
				hashedPassword, err := bcrypt.GenerateFromPassword(
					[]byte(password), bcrypt.DefaultCost)
				if err != nil {
					return err
				}
				objs[name] = hashedPassword
			}
		case bucketUsers:
			requestData := make(map[string]*User)
			// производим непосредственный разбор данных
			if err := c.Bind(&requestData); err != nil {
				return err
			}
			// инициализируем результирующий список
			objs = make(map[string][]byte, len(requestData))
			// перебираем всех пользователей
			for name, user := range requestData {
				// проверяем, что имя пользователя соответствует формату email
				if !ValidateEmail(name) {
					return c.Error(http.StatusBadRequest,
						fmt.Sprintf("%s is not email", name))
				}
				// пользователи без определения объекта будут автоматически
				// удалены, поэтому с ними ничего делать больше не нужно
				if user == nil {
					objs[name] = nil
					continue
				}
				if user.Group == "" {
					return c.Error(http.StatusBadRequest,
						fmt.Sprintf("user %s group required", name))
				}
				if user.Password == "" {
					return c.Error(http.StatusBadRequest,
						fmt.Sprintf("user %s password required", name))
				}
				// проверяем, что пароль задан в виде хеша
				if _, err := bcrypt.Cost([]byte(user.Password)); err != nil {
					// хешируем пароль, если это не так
					hashedPassword, err := bcrypt.GenerateFromPassword(
						[]byte(user.Password), bcrypt.DefaultCost)
					if err != nil {
						return err
					}
					user.Password = string(hashedPassword)
				}
				// добавляем время обновления
				user.Updated = time.Now().UTC()
				// сериализуем данные пользователя
				data, err := json.MarshalIndent(user, "", "    ")
				if err != nil {
					return err
				}
				objs[name] = data
			}
		default:
			requestData := make(map[string]rest.JSON)
			// производим непосредственный разбор данных
			if err := c.Bind(&requestData); err != nil {
				return err
			}
			// инициализируем результирующий список
			objs = make(map[string][]byte, len(requestData))
			for name, obj := range requestData {
				// сериализуем данные описания
				data, err := json.MarshalIndent(obj, "", "    ")
				if err != nil {
					return err
				}
				objs[name] = data
			}
		}
		// сохраняем обработанные данные
		return s.save(section, objs)
	}
}

// put сохраняет или изменяем объект с указанным именем в разделе хранилища.
func (s *Store) put(section, name string, data []byte) error {
	// проверяем, что данные не пустые
	if data == nil {
		return rest.ErrBadRequest

	}
	return s.db.Update(func(tx *bolt.Tx) error {
		// инициализируем раздел, если он не был создан ранее
		bucket, err := tx.CreateBucketIfNotExists([]byte(section))
		if err != nil {
			return err
		}
		// сохраняем данные о сервисе в хранилище
		return bucket.Put([]byte(name), data)
	})
}

// Put позволяет
func (s *Store) Put(section string) rest.Handler {
	return func(c *rest.Context) error {
		var (
			name = c.Param("name")
			data []byte
			err  error
		)
		// в зависимости от раздела поразному обрабатываем входящие данные
		switch section {
		case bucketAdmins:
			requestData := new(struct {
				Password string `json:"password"`
			})
			if err = c.Bind(requestData); err != nil {
				break
			}
			// проверяем, что это хеш от пароля, а не сам пароль
			// если это не так, то хешируем пароль
			if _, err = bcrypt.Cost([]byte(requestData.Password)); err == nil {
				// пароль уже представлен в виде хеша
				data = []byte(requestData.Password)
				break
			}
			// хешируем пароль
			data, err = bcrypt.GenerateFromPassword(
				[]byte(requestData.Password), bcrypt.DefaultCost)
		case bucketUsers:
			// проверяем, что имя пользователя соответствует формату email
			if !ValidateEmail(name) {
				return c.Error(http.StatusBadRequest,
					fmt.Sprintf("%s is not email", name))
			}
			user := new(User)
			if err = c.Bind(user); err != nil {
				break
			}
			if user.Group == "" {
				return c.Error(http.StatusBadRequest,
					fmt.Sprintf("user %s group required", name))
			}
			if user.Password == "" {
				return c.Error(http.StatusBadRequest,
					fmt.Sprintf("user %s password required", name))
			}
			// проверяем, что пароль задан в виде хеша
			if _, err = bcrypt.Cost([]byte(user.Password)); err != nil {
				// хешируем пароль, если это не так
				data, err = bcrypt.GenerateFromPassword(
					[]byte(user.Password), bcrypt.DefaultCost)
				if err != nil {
					break
				}
				user.Password = string(data)
			}
			// добавляем время обновления
			user.Updated = time.Now().UTC()
			// сериализуем данные описания
			data, err = json.MarshalIndent(user, "", "    ")
		default:
			requestData := make(rest.JSON)
			if err = c.Bind(&requestData); err != nil {
				break
			}
			// сериализуем данные описания
			data, err = json.MarshalIndent(requestData, "", "    ")
		}
		if err != nil {
			return err
		}
		// сохраняем данные в хранилище
		return s.put(section, name, data)
	}
}

// config возвращает объединенную конфигурацию пользователя. Если указан
// пароль, то он проверяется на совпадение с паролем пользователя.
func (s *Store) config(username, password string) (interface{}, error) {
	var result interface{}
	// делаем выборку из хранилища данных
	err := s.db.View(func(tx *bolt.Tx) error {
		// инициализируем доступ к разделу с данными пользователей
		bucket := tx.Bucket([]byte(bucketUsers))
		if bucket == nil {
			return rest.ErrNotFound
		}
		// запрашиваем данные о пользователе из хранилища
		data := bucket.Get([]byte(username))
		// проверяем, что такой пользователь зарегистрирован
		if data == nil {
			return rest.ErrNotFound
		}
		// десериализуем данные о пользователе из хранилища
		user := new(User)
		if err := json.Unmarshal(data, user); err != nil {
			return err
		}
		// проверяем пароль пользователя, если он указан
		if password != "" {
			// проверяем пароль пользователя
			if err := bcrypt.CompareHashAndPassword(
				[]byte(user.Password), []byte(password)); err != nil {
				if err == bcrypt.ErrMismatchedHashAndPassword {
					return rest.ErrForbidden
				}
				return err
			}
		}

		result = user.Services

		// разбираем с группой, в которую входит пользователь
		bucket = tx.Bucket([]byte(bucketGroups))
		// если информации о группах в хранилище нет,
		// то отдаем информацию о сервисах пользователя "как есть"
		if bucket == nil {
			return nil
		}
		// запрашиваем данные из хранилища о группе пользователя
		data = bucket.Get([]byte(user.Group))
		// если информации о группе пользователя в хранилище нет,
		// то отдаем информацию о сервисах пользователя "как есть"
		if data == nil {
			return nil
		}
		// десериализуем информацию о сервисах группы
		groupServices := make(map[string]rest.JSON)
		if err := json.Unmarshal(data, &groupServices); err != nil {
			return err
		}
		// если сервисы для группы не определены,
		// то отдаем информацию о сервисах пользователя "как есть"
		if len(groupServices) == 0 {
			return nil
		}

		// разбираемся с конфигурациями сервисов
		bucket = tx.Bucket([]byte(bucketServices))
		if bucket == nil {
			return nil
		}
		// инициализируем итоговую конфигурацию пользователя
		config := make(map[string]rest.JSON)
		// перебираем все сервисы группы
		for name, groupData := range groupServices {
			// запрашиваем данные из хранилища о конфигурации сервиса
			data = bucket.Get([]byte(name))
			// если сервис не определен, то добавляем только данные из группы
			if data == nil {
				config[name] = groupData
				continue
			}
			// десериализуем данные о сервисе
			service := make(rest.JSON)
			if err := json.Unmarshal(data, &service); err != nil {
				return err
			}

			// дополняем конфигурацию сервиса данными из группы
			for serviceName, data := range groupData {
				service[serviceName] = data
			}
			// дополняем конфигурацию сервиса данными самого пользователя
			for serviceName, data := range user.Services[name] {
				service[serviceName] = data
			}
			// сохраняем полученную конфигурацию сервиса
			config[name] = service
			// чтобы потом не дублировать, удаляем информацию о сервисе
			// из данных пользователя
			delete(user.Services, name)
		}
		// добавляем оставшиеся описания сервисов пользователя
		for serviceName, data := range user.Services {
			config[serviceName] = data
		}
		result = config
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Config возвращает конфигурацию сервисов пользователя, объединив ее со
// всеми данными хранилища.
func (s *Store) Config(c *rest.Context) error {
	// получаем информацию об авторизации запроса
	username, password, ok := c.BasicAuth()
	// проверяем, что запрос с авторизацией
	if !ok {
		realm := fmt.Sprintf("Basic realm=%s", appName)
		c.SetHeader("WWW-Authenticate", realm)
		return rest.ErrUnauthorized
	}
	// получаем конфигурацию
	config, err := s.config(username, password)
	if err != nil {
		return err
	}
	return c.Write(config)
}

// UserConfig отдает авторизацию пользователя, указанного в запросе.
func (s *Store) UserConfig(c *rest.Context) error {
	config, err := s.config(c.Param("name"), "")
	if err != nil {
		return err
	}
	return c.Write(config)
}

// Backup возвращает содержимое хранилища в виде JSON.
func (s *Store) Backup(c *rest.Context) error {
	return s.db.View(func(tx *bolt.Tx) error {
		backup := make(rest.JSON)
		err := tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			service := make(rest.JSON)
			err := b.ForEach(func(k, v []byte) error {
				switch string(name) {
				case bucketAdmins:
					service[string(k)] = string(v)
				default:
					service[string(k)] = json.RawMessage(v)
				}
				return nil
			})
			if err != nil {
				return err
			}
			backup[string(name)] = service
			return nil
		})
		if err != nil {
			return err
		}
		return c.Write(backup)
	})
}
