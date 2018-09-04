package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	app "github.com/mdigger/app-info"
	"github.com/mdigger/log"
	"github.com/mdigger/rest"
)

var (
	appName = "provisioning" // название сервиса
	version = "2.0"          // версия
	commit  string           // идентификатор GIT
	date    string           // дата сборки
)

func main() {
	var ahost = flag.String("admin", app.Env("ADMIN", "localhost:8000"),
		"admin server address and `port`")
	var httphost = flag.String("port", app.Env("PORT", ":8000"),
		"http server `port`")
	var letsencrypt = flag.String("letsencrypt", app.Env("LETSENCRYPT_HOST", ""),
		"domain `host` name")
	var dbname = appName + ".db" // имя файла с хранилищем
	flag.StringVar(&dbname, "db", dbname, "store `filename`")
	flag.Parse()

	// выводим в лог информацию о версии сервиса
	app.Parse(appName, version, commit, date)
	log.Info("service", app.LogInfo())

	// разбираем имя хоста и порт, на котором будет слушать веб-сервер
	port, err := app.Port(*httphost)
	if err != nil {
		log.Error("http host parse error", err)
		os.Exit(2)
	}

	log.Info("opening store", "file", dbname)
	store, err := OpenStore(dbname)
	if err != nil {
		log.Error("opening store error", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	var adminMux = &rest.ServeMux{
		Headers: map[string]string{
			"Server":            "Provisioning admin/2.0",
			"X-API-Version":     "1.1",
			"X-Service-Version": version,
		},
		Logger: log.New("admin"),
	}
	adminMux.Handles(rest.Paths{
		"/services": rest.Methods{
			"GET": store.List(sectionServices),
		},
		"/services/:name": rest.Methods{
			"GET":    store.Item(sectionServices),
			"PUT":    store.Update(sectionServices),
			"DELETE": store.Remove(sectionServices),
		},
		"/groups": rest.Methods{
			"GET": store.List(sectionGroups),
		},
		"/groups/:name": rest.Methods{
			"GET":    store.Item(sectionGroups),
			"PUT":    store.Update(sectionGroups),
			"DELETE": store.Remove(sectionGroups),
		},
		"/users": rest.Methods{
			"GET": store.List(sectionUsers),
		},
		"/users/:name": rest.Methods{
			"GET":    store.Item(sectionUsers),
			"PUT":    store.Update(sectionUsers),
			"DELETE": store.Remove(sectionUsers),
		},
		"/users/:name/config": rest.Methods{
			"GET": store.UserConfig,
		},
		"/users/:name/data": rest.Methods{
			"GET":    store.Item(sectionUserData),
			"PUT":    store.Update(sectionUserData),
			"DELETE": store.Remove(sectionUserData),
			"PATCH":  store.UserDataPatch,
		},
		"/admins": rest.Methods{
			"GET": store.List(sectionAdmins),
		},
		"/admins/:name": rest.Methods{
			"GET":    store.Item(sectionAdmins),
			"PUT":    store.Update(sectionAdmins),
			"DELETE": store.Remove(sectionAdmins),
		},
		"/gmail": rest.Methods{
			"GET": store.GetGmailConfig,
			"PUT": store.SetGmailConfig,
		},
		"/templates": rest.Methods{
			"GET": store.List(sectionTemplates),
		},
		"/templates/:name": rest.Methods{
			"GET":    store.Item(sectionTemplates),
			"PUT":    store.Update(sectionTemplates),
			"DELETE": store.Remove(sectionTemplates),
		},
		"/templates/:name/send/:to": rest.Methods{
			"POST": store.SendWithTemplate,
		},
		"/backup": rest.Methods{
			"GET": store.Backup,
		},
	}, store.AdminAuth) // все запросы требуют авторизации администратора

	// инициализируем HTTP-сервер для административной части сервиса
	aserver := &http.Server{
		Addr:         *ahost,
		Handler:      adminMux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 20,
	}
	go func() {
		log.Info("starting admin server",
			"address", aserver.Addr)
		err = aserver.ListenAndServe()
		if err != nil {
			log.Warn("admin server stoped", "error", err)
			os.Exit(3)
		}
	}()

	// инициализируем обработку HTTP запросов
	var httplogger = log.New("http")
	var mux = &rest.ServeMux{
		Headers: map[string]string{
			"Server":                      app.Agent,
			"X-API-Version":               "1.1",
			"X-Service-Version":           version,
			"Access-Control-Allow-Origin": "*",
		},
		Logger: httplogger,
	}
	mux.Handle("GET", "/config", store.Config)
	mux.Handle("POST", "/reset/:name", store.PasswordToken)
	mux.Handle("POST", "/password", store.SetUserPassword)
	mux.Handle("POST", "/password/:token", store.ResetPassword)
	mux.Handle("GET", "/data", store.UserData)

	var server = &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 20,
		ErrorLog:     httplogger.StdLog(log.ERROR),
	}
	var hosts []string
	// настраиваем автоматическое получение сертификата
	if *letsencrypt != "" {
		hosts = strings.Split(*letsencrypt, ",")
		server.TLSConfig = app.LetsEncrypt(hosts...)
		server.Addr = ":443" // подменяем порт на 443
	} else {
		tlsConfig, err := app.LoadCertificates(filepath.Join(".", "certs"))
		if err != nil {
			httplogger.Error("certificates error", err)
			os.Exit(2)
		}
		if tlsConfig != nil {
			server.TLSConfig = tlsConfig
			hosts = make([]string, 0, len(tlsConfig.NameToCertificate))
			for name := range tlsConfig.NameToCertificate {
				hosts = append(hosts, name)
			}
		}
	}

	// отслеживаем сигнал о прерывании и останавливаем по нему сервер
	go func() {
		var sigint = make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint
		if err := server.Shutdown(context.Background()); err != nil {
			httplogger.Error("server shutdown", err)
		}
	}()
	// добавляем в статистику и выводим в лог информацию о запущенном сервере
	if server.TLSConfig != nil {
		// добавляем заголовок с обязательством использования защищенного
		// соединения в ближайший час
		mux.Headers["Strict-Transport-Security"] = "max-age=3600"
	}
	httplogger.Info("server",
		"listen", server.Addr,
		"tls", server.TLSConfig != nil,
		"hosts", hosts,
		"letsencrypt", *letsencrypt != "",
	)
	defer log.Info("service stoped")

	// в зависимости от того, поддерживаются сертификаты или нет, запускается
	// разная версию веб-сервера
	if server.TLSConfig != nil {
		err = server.ListenAndServeTLS("", "")
	} else {
		err = server.ListenAndServe()
	}
	if err != http.ErrServerClosed {
		httplogger.Error("server", err)
	} else {
		httplogger.Info("server stopped")
	}
}
