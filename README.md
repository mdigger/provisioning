# Сервис провижининга устройств

## Данные

### Определение сервисов

    "MX": {
        "address": "89.185.256.135",
        "CSTA Port": "7778",
        "CSTA SSL": true,
        "version": 6
    },
    "MXStore": {
        "address": "89.185.246.177",
        "version": 2
    },
    "PushServer": {
        "address": "89.185.246.174",
        "version": 1
    }

Для каждого сервиса необходимо указать его имя и объект с именованными параметрами. Пример выше задает три именованных сервиса: `MX`, `MXStore` и `PushServer`.


### Определение группы

    "group1": {
        "MX": {
            "test": true,
            "version": 7
        },
        "MXStore": null,
        "PushServer": null
    }

Группа пользователей описывает имена сервисов, которые в нее входят (в виде ключей) и, опционально, может содержать список дополнительных параметров сервиса, которые будут добавлены или перезаписаны в итоговой конфигурации. Если переопределять параметры сервиса не требуется, то они могут быть опущены.

В данном примере у нас для группы пользователей определены три сервиса: `MX`, `MXStore` и `PushServer`. Причем, для сервиса `MX` добавлен параметр `test` и переопределен параметра `Version`.


### Пользователь

    "dmitrys@xyzrd.com": {
        "password": "$2a$10$OC8LKbl.fU6xVh.o0bVktejzQwvkzGtkOSZ73GYZBAI1Q872FUUPK",
        "group": "group1",
        "services": {
            "MX": {
                "login": {
                    "password": "password",
                    "user": "dmitrys"
                }
            },
            "Test-Service": {
                "version": 99
            }
        }
    }

Пользователь описывается двумя обязательными не пустыми полями: `password` и `group`.  

В поле `password` содержится **bcrypt**-hash от заданного пароля. Если пароль задан в виде открытого текста, то при сохранении он автоматически заменяется на hash и его восстановление или получение в исходном виде уже не доступно. Пароль не может быть пустым.

Поле `group` содержит название группы, описывающей включаемые для данного пользователя сервисы. Это поле является обязательным и не может быть пустым. Хотя обязательное существование такой группы при этом не проверяется.

Необязательное поле `services` может содержать именованный список сервисов пользователя с дополнительными параметрами для них, которые автоматически добавляются к параметрам сервиса при отдаче конфигурации. Так же здесь могут содержаться и описания дополнительных личных сервисов пользователя.


### Получение конфигурации

    {
        "MX": {
            "address": "89.185.256.135",
            "CSTA Port": "7778",
            "CSTA SSL": true,
            "version": 7,
            "login": {
                "password": "password",
                "user": "dmitrys"
            },
            "test": true
        },
        "MXStore": {
            "address": "89.185.246.177",
            "version": 2
        },
        "PushServer": {
            "address": "89.185.246.174",
            "version": 1
        },
        "Test-Service": {
            "version": 99
        }
    }

Для получения итоговой конфигурации сервисов пользователя запрос должен быть с авторизацией: имя пользователя берется именно из него.

Итоговая конфигурация складывается из параметров самих сервисов, которые определены для группы пользователя. После этого на все параметры накладываются их переопределения для группы пользователей, если они заданы. И, в конце концов, добавляются параметры самого пользователя для сервисов. Таким образом, на каждом этапе любые сервисы могут быть как добавлены, так и переопределены или добавлены любые их параметры. Удаление сервисов или параметров при этом невозможно.

Стоит обратить внимание, что названия сервисов, групп, имен пользователей и ключей сервисов регистрозависимые и могут сосуществовать. Например: `Version` и `version` — это два разных ключа.


## REST API

Для изменения сервисов, групп и пользователей используется URL `/admin/`. Если задан хотя бы один администратор системы, до доступ к этим данным требует авторизации, которая передается в заголовке **HTTP Basic** запроса.

Для всех типов информации поддерживаются следующие пути:

- `/admin/auth` - информация об администраторах системы
- `/admin/service` - доступ к описанию сервисов
- `/admin/groups` - доступ к описанию групп пользователей
- `/admin/users` - доступ к информации о пользователях

В зависимости от метода запроса, выполняются разные действия:

- `GET /admin/<section>` возвращает список зарегистрированных администраторов, сервисов, групп и пользователей

- `POST /admin/<section>` позволяет добавить или изменить сразу несколько сервисов, групп или пользователей. Данные передаются в формате **JSON**. В качестве имен используются корневые ключи. Например, так можно задать или изменить сразу несколько описаний сервисов:

        {
            "MX": {
                "address": "89.185.256.135",
                "CSTA Port": "7778",
                "CSTA SSL": true,
                "version": 6
            },
            "MXStore": {
                "address": "89.185.246.177",
                "version": 2
            },
            "PushServer": {
                "address": "89.185.246.174",
                "version": 1
            }
        }

    Если необходимо какой-либо из сервисов удалить, то в качестве его параметров необходимо указать значение `null`. Если необходимо добавить сервис, которые не требует определение параметров, то можно указать пустой список - `{}`.

    Для задания администраторов задается только список паролей:

        {
            "admin": "password",
            "admin@xyzrd.com": "password"
        }

- `GET /admin/<section>/<name>` - возвращает описание сервиса, группы или пользователя, сохраненного в хранилище данных. Для авторизационной информации администраторов пароль не возвращается: вместо этого отдается объект со значением `{"exists": true}`.

- `PUT /admin/<section>/<name>` - позволяет изменить описание сервиса, группы или пользователя, или задать пароль администратора. Параметры передаются в теле запроса в виде JSON:

        {
            "address": "89.185.256.135",
            "CSTA Port": "7778",
            "CSTA SSL": true,
            "version": 6
        }

    Для задания пароля администратора необходимо указать только пароль:

        {
            "password": "new password"
        }

- `DELETE /admin/<section>/<name>` - позволяет удалить описание сервиса, пользователя, группы или администратора. Никаких дополнительных параметров в запросе не требуется.

Статус выполнения запроса можно отслеживать по кодам возврата HTTP. Для ошибко так же возвращается их описание в виде объекта JSON:
    
    {
        "error": "description"
    }

Для создания или изменения пользователя задана жесткая схема:

    {
        "name": "Dmitry Sedykh",
        "password": "$2a$10$p1VcgtJUMUxZFG3eWxUIaOB70A.ymwzQS2BpXwImMjZapf3JAutz2",
        "group": "group1",
        "services": {
            "MX": {
                "login": {
                    "password": "password",
                    "user": "dmitrys"
                }
            },
            "Test-Service": {
                "version": 99
            }
        }
    }

- `name` - необязательное имя пользователя
- `password` - пароль пользователя в виде **bcrypt**-hash. Если пароль задан в виде простого текста, то при сохранении он автоматически будет заменен на хеш пароля. Пароль не может быть пустым.
- `group` - обязательное не пустое название группы пользователей, описания сервисов которой будет использоваться для получения конфигурации.
- `services` - необязательный дополнительный список параметров сервисов пользователя.

### Получение конфигурации

Для получения конфигурации пользователя необходимо сделать запрос: 
    
- `GET /config`

Запрос требует авторизации пользователя на получение конфигурации. Авторизационная информация должна быть передана в заголовке запроса **HTTP Basic**.


## Примеры запросов с cURL

### Администраторы

- Возвращает список имен администраторов

    ```sh
    curl "https://<service.name>/admin/auth" \
         -u admin:password
    ```

- Добавление списка администраторов

    ```sh
    curl -X "POST" "https://<service.name>/admin/auth" \
         -H "Content-Type: application/json; charset=utf-8" \
         -u admin:password \
         -d $'{
      "admin": "password",
      "dmitrys@xyzrd.com": "__xflash24"
    }'
    ```

- Изменение пароля администратора

    ```sh
    curl -X "PUT" "https://<service.name>/admin/auth/<name>" \
         -H "Content-Type: application/json; charset=utf-8" \
         -u admin:password \
         -d $'{
      "password": "password"
    }'
    ```

- Удаление администратора

    ```sh
    curl -X "DELETE" "https://<service.name>/admin/auth/<name>" \
         -u admin:password
    ```

- Проверка администратора

    ```sh
    curl -X GET "https://<service.name>/admin/auth/<name>" \
        -u "admin":"password"
    ```

### Описания сервисов

- Получение списка сервисов

    ```sh
    curl -X GET "https://<service.name>/admin/services" \
        -u "admin":"password"
    ```

- Добавление или изменение описания сервисов

    ```sh
    curl -X POST "https://<service.name>/admin/services" \
        -H "Content-Type: text/plain; charset=utf-8" \
        -u "admin":"password"
        -d $'{
          "MX": {
            "Address": "89.185.256.135",
            "CSTA Port": "7778",
            "CSTA SSL": true,
            "Version": 6
          },
          "MXStore": {
            "Address": "89.185.246.177",
            "Version": 2
          },
          "PushServer": {
            "Address": "89.185.246.174",
            "Vestion": 1
          },
          "Test-Service": null
        }'
    ```

- Изменение описание сервиса

    ```sh
    curl -X PUT "https://<service.name>/admin/services/<name>" \
        -H "Content-Type: application/json; charset=utf-8" \
        -u "admin":"password"
        -d $'{
          "name": "Test Service",
          "param1": "33"
        }'
    ```

- Удаление описания сервиса

    ```sh
    curl -X DELETE "https://<service.name>/admin/services/<name>" \
        -H "Content-Type: text/plain" \
        -u "admin":"password"
    ```

- Получение описания сервиса

    ```sh
    curl -X GET "https://<service.name>/admin/services/<name>" \
        -u "admin":"password"
    ```

### Группы пользователей

- Получение списка групп пользователей

    ```sh
    curl "https://<service.name>/admin/groups" \
         -u admin:"password"
    ```

- Изменение или добавление нескольких групп пользователей

    ```sh
    curl -X "POST" "https://<service.name>/admin/groups" \
         -H "Content-Type: application/json; charset=utf-8" \
         -u admin:"password" \
         -d $'{
      "group2": {
        "MXStore": {}
      },
      "group1": {
        "MXStore": null,
        "PushServer": null,
        "MX": {
          "Version": 7,
          "test": true
        }
      }
    }'
    ```

- Изменение одной группы пользователей

    ```sh
    curl -X "PUT" "https://<service.name>/admin/groups/<name>" \
         -H "Content-Type: application/json; charset=utf-8" \
         -u admin:"password" \
         -d $'{
      "MXStore": null,
      "PushServer": {},
      "MX": {}
    }'
    ```

- Удаление группы пользователей

    ```sh
    curl -X "DELETE" "https://<service.name>/admin/groups/<name>" \
         -u admin:"password"
    ```

- Получение описания группы пользователей

    ```sh
    curl "https://<service.name>/admin/groups/<name>" \
         -u admin:"password"
    ```

### Пользователи

- Получение списка пользователей

    ```sh
    curl "https://<service.name>/admin/users" \
         -u admin:"password"
    ```

- Изменение или добавление сразу нескольких пользователей

    ```sh
    curl -X "POST" "https://<service.name>/admin/users" \
         -H "Content-Type: application/json; charset=utf-8" \
         -u admin:"password" \
         -d $'{
          "dmitrys@xyzrd.com": {
            "group": "group1",
            "password": "password",
            "Name": "Dmitry Sedykh",
            "services": {
              "Test-Service": {
                "version": 99
              },
              "MX": {
                "login": {
                  "user": "dmitrys",
                  "password": "password"
                }
              }
            }
          },
          "maximd@xyzrd.com": {
            "group": "group2",
            "password": "password"
          },
          "test@xyzrd.com": null
        }'
    ```

- Изменение пользователя

    ```sh
    curl -X "PUT" "https://<service.name>/admin/users/<name>" \
         -H "Content-Type: application/json; charset=utf-8" \
         -u admin:"password" \
         -d $'{
          "group": "group1",
          "name": "Dmitry Sedykh",
          "password": "password"
        }'
    ```

- Удаление пользователя

    ```sh
    curl -X "DELETE" "https://<service.name>/admin/users/<name>" \
         -u admin:"password"
    ```

- Получение описания пользователя

    ```sh
    curl "https://<service.name>/admin/users/<name>" \
         -u admin:"password"
    ```

### Конфигурация для устройства

```sh
curl "https://<service.name>/config" \
     -u dmitrys@xyzrd.com:"password"
```
