package main

import (
	"encoding/json"
	"time"

	"github.com/boltdb/bolt"
	"github.com/mdigger/rest"
)

// Store описывает хранилище данных и доступ к нему.
type Store struct {
	db *bolt.DB
}

// OpenStore открывает хранилище данных.
func OpenStore(dsn string) (*Store, error) {
	db, err := bolt.Open(dsn, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close закрывает хранилище данных.
func (s *Store) Close() error {
	return s.db.Close()
}

// Keys возвращает список ключей в указанном разделе хранилища. Если раздел
// хранилища не существует, то будет возвращен пустой список.
func (s *Store) Keys(section string) ([]string, error) {
	list := []string{} // хочется, чтобы в JSON отображался пустой список, а не null
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(section))
		if bucket == nil {
			return nil
		}
		// инициализируем список, сразу задавая его размер
		list = make([]string, 0, bucket.Stats().KeyN)
		return bucket.ForEach(func(k, v []byte) error {
			// // игнорируем пустышки
			// if v != nil {
			list = append(list, string(k))
			// }
			return nil
		})
	})
	return list, err
}

// Remove удаляет из секции хранилища указанные ключи. Если какие-то ключи
// в секции не найдены, то ошибки не возникает.
func (s *Store) Remove(section string, keys ...string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(section))
		if bucket == nil {
			return nil
		}
		bucketPasswords := tx.Bucket([]byte(PasswordSection))
		for _, key := range keys {
			if err := bucket.Delete([]byte(key)); err != nil {
				return err
			}
			// удаляем пароли пользователей при удалении пользователя
			if section == "users" && bucketPasswords != nil {
				if err := bucket.Delete([]byte(key)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Save сохраняет в коллекции значения с указанными ключами. Ключи с пустыми
// значениями данных удаляются.
func (s *Store) Save(section string, values map[string]interface{}) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		// инициализируем раздел, если он не был создан ранее
		bucket, err := tx.CreateBucketIfNotExists([]byte(section))
		if err != nil {
			return err
		}
		bucketPasswords, err := tx.CreateBucketIfNotExists([]byte(PasswordSection))
		if err != nil {
			return err
		}
		// сохраняем все значения, используя имя в качестве ключа
		for name, value := range values {
			var data []byte
			switch obj := value.(type) {
			case nil:
				// удаляем пустые ключи
				err := bucket.Delete([]byte(name))
				if err != nil {
					return err
				}
				// удаляем сохраненный пароль пользователя
				if section == "users" {
					if err := bucketPasswords.Delete([]byte(name)); err != nil {
						return err
					}
				}
				continue
			case []byte:
				// для бинарных данных трансформация в JSON не требуется
				data = obj
			default:
				// по умолчанию преобразуем данные в формат JSON
				data, err = json.MarshalIndent(value, "", "    ")
				if err != nil {
					return err
				}
				// задаем пароль пользователя
				if section == "users" && bucketPasswords.Get([]byte(name)) == nil {
					err := bucketPasswords.Put([]byte(name), generatePassword())
					if err != nil {
						return err
					}
				}
			}
			err := bucket.Put([]byte(name), data)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Get возвращает из указанного хранилища значения ключей. Возвращаются
// только те ключи, которые имею не пустое значение в хранилище.
func (s *Store) Get(section string, keys ...string) (map[string]json.RawMessage, error) {
	values := make(map[string]json.RawMessage, len(keys))
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(section))
		if bucket == nil {
			return nil
		}
		// перебираем все ключи и заполняем данными
		// т.к. данные уже хранятся в формате JSON, то обратного их
		// преобразования не требуется — берем как есть
		for _, key := range keys {
			// заполняем только не пустыми значениями
			value := bucket.Get([]byte(key))
			if value != nil {
				values[key] = json.RawMessage(value)
			}
		}
		return nil
	})
	return values, err
}

func (s *Store) Backup(c *rest.Context) error {
	return s.db.View(func(tx *bolt.Tx) error {
		backup := make(rest.JSON)
		err := tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			service := make(rest.JSON)
			err := b.ForEach(func(k, v []byte) error {
				switch string(name) {
				case AdminSection, PasswordSection:
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
