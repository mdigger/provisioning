package main

import (
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/mdigger/log"
	"github.com/mdigger/rest"
)

var (
	appName = "provisioning"           // название сервиса
	version = "1.0.17"                 // версия
	date    = "2017-05-30"             // дата сборки
	host    = "config.connector73.net" // имя сервера
	ahost   = "localhost:8000"         // адрес административного сервера и порт
)

func main() {
	var dbname = appName + ".db" // имя файла с хранилищем
	flag.StringVar(&ahost, "admin", ahost, "admin server address and `port`")
	flag.StringVar(&host, "host", host, "main server `name`")
	flag.StringVar(&dbname, "db", dbname, "store `filename`")
	flag.Var(log.Flag(), "log", "log `level`")
	flag.Parse()

	log.Info("starting service",
		"version", version,
		"date", date,
		"name", appName)

	log.Info("opening store", "file", dbname)
	store, err := OpenStore(dbname)
	if err != nil {
		log.Error("opening store error", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	var adminMux = &rest.ServeMux{
		Headers: map[string]string{
			"Server":            "Provisioning admin/1.0",
			"X-API-Version":     "1.0",
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
		Addr:         ahost,
		Handler:      adminMux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 20,
	}
	go func() {
		var (
			secure = true                            // запускать с TLS
			serts  = []string{"cert.pem", "key.pem"} // файлы с сертификатами
		)
		// если файлы с сертификатами отсутствуют, то не запускать TLS
		for _, name := range serts {
			if _, err := os.Stat(name); err != nil {
				secure = false
				break
			}
		}
		log.Info("starting admin server",
			"address", aserver.Addr,
			"https", secure)
		// в зависимости от наличия сертификатов запускается в соответствующем
		// режиме
		var err error
		if secure {
			err = aserver.ListenAndServeTLS(serts[0], serts[1])
		} else {
			err = aserver.ListenAndServe()
		}
		if err != nil {
			log.Warn("admin server stoped", "error", err)
			os.Exit(3)
		}
	}()

	var mux = &rest.ServeMux{
		Headers: map[string]string{
			"Server":            "Provisioning/1.0",
			"X-API-Version":     "1.0",
			"X-Service-Version": version,
		},
		Logger: log.New("http"),
	}
	mux.Handle("GET", "/config", store.Config)
	mux.Handle("POST", "/reset/:name", store.PasswordToken)
	mux.Handle("POST", "/password", store.SetUserPassword)
	mux.Handle("POST", "/password/:token", store.ResetPassword)
	mux.Handle("GET", "/data", store.UserData)

	server := &http.Server{
		Addr:         host,
		Handler:      mux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 20,
	}
	if !strings.HasPrefix(host, "localhost") &&
		!strings.HasPrefix(host, "127.0.0.1") {
		manager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(host),
			Email:      "dmitrys@xyzrd.com",
			Cache:      autocert.DirCache("letsEncript.cache"),
		}
		server.TLSConfig = &tls.Config{
			GetCertificate: manager.GetCertificate,
		}
		server.Addr = ":https"
	}

	go func() {
		var secure = (server.Addr == ":https" || server.Addr == ":443")
		slog := log.With("address", server.Addr, "https", secure)
		if server.Addr != host {
			slog = slog.With("host", host)
		}
		slog.Info("starting main server")
		if secure {
			err = server.ListenAndServeTLS("", "")
		} else {
			err = server.ListenAndServe()
		}
		if err != nil {
			log.Warn("main server stoped", "error", err)
			os.Exit(3)
		}
	}()

	monitorSignals(os.Interrupt, os.Kill)
	log.Info("service stoped")
}

// monitorSignals запускает мониторинг сигналов и возвращает значение, когда
// получает сигнал. В качестве параметров передается список сигналов, которые
// нужно отслеживать.
func monitorSignals(signals ...os.Signal) os.Signal {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, signals...)
	return <-signalChan
}
