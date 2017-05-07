package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/mdigger/log"
	"github.com/mdigger/rest"
)

var (
	appName = "provisioning"                 // название сервиса
	version = "0.2.1"                        // версия
	date    = "2017-05-07"                   // дата сборки
	build   = ""                             // номер сборки в git-репозитории
	host    = "provisioning.connector73.net" // имя сервера
	ahost   = "localhost:8000"               // адрес административного сервера и порт
)

func main() {
	log.SetLevel(log.DebugLevel)
	log.SetFlags(0)
	// выводим информацию о версии сборки
	log.WithFields(log.Fields{
		"version": version,
		"date":    date,
		"build":   build,
		"name":    appName,
	}).Info("starting service")

	// разбираем параметры запуска приложения
	dbname := flag.String("db", appName+".db", "store `filename`")
	flag.StringVar(&host, "host", host, "main server `name`")
	flag.StringVar(&ahost, "admin", ahost, "admin server address and `port`")
	flag.Parse()

	// открываем хранилище данных
	log.WithField("file", *dbname).Info("opening store")
	store, err := OpenStore(*dbname)
	if err != nil {
		log.WithError(err).Error("opening store error")
		os.Exit(1)
	}
	defer store.Close()

	// инициализируем мультиплексор HTTP-запросов
	var amux = &rest.ServeMux{
		Headers: map[string]string{
			"Server":            "Provisioning admin/1.0",
			"X-API-Version":     "1.0",
			"X-Service-Version": version,
		},
		Logger:  log.Default,
		Encoder: Encoder, // переопределяем формат вывода
	}
	// задаем обработчики запросов
	amux.Handles(rest.Paths{
		// обработчики администраторов
		"/auth": rest.Methods{
			"GET":  store.List(bucketAdmins),
			"POST": store.Post(bucketAdmins),
		},
		"/auth/:name": rest.Methods{
			"GET":    store.Get(bucketAdmins),
			"PUT":    store.Put(bucketAdmins),
			"DELETE": store.Remove(bucketAdmins),
		},
		// обработчики сервисов
		"/services": rest.Methods{
			"GET":  store.List(bucketServices),
			"POST": store.Post(bucketServices),
		},
		"/services/:name": rest.Methods{
			"GET":    store.Get(bucketServices),
			"PUT":    store.Put(bucketServices),
			"DELETE": store.Remove(bucketServices),
		},
		// обработчики групп пользователей
		"/groups": rest.Methods{
			"GET":  store.List(bucketGroups),
			"POST": store.Post(bucketGroups),
		},
		"/groups/:name": rest.Methods{
			"GET":    store.Get(bucketGroups),
			"PUT":    store.Put(bucketGroups),
			"DELETE": store.Remove(bucketGroups),
		},
		// обработчики пользователей
		"/users": rest.Methods{
			"GET":  store.List(bucketUsers),
			"POST": store.Post(bucketUsers),
		},
		"/users/:name": rest.Methods{
			"GET":    store.Get(bucketUsers),
			"PUT":    store.Put(bucketUsers),
			"DELETE": store.Remove(bucketUsers),
		},
		// настройки почты
		"/gmail": rest.Methods{
			"GET":  store.MailConfig,
			"POST": store.SetMailConfig,
		},
		// настройки шаблонов почты
		"/gmail/template": rest.Methods{
			"GET":  store.MailTemplate,
			"POST": store.StoreTemplate,
		},
		// отдача конфигурации всех сервисов и пользователей
		"/backup": rest.Methods{
			"GET": store.Backup,
		},
	}, store.CheckAdmins)

	// инициализируем HTTP-сервер для административной части сервиса
	aserver := &http.Server{
		Addr:         ahost,
		Handler:      amux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 20,
	}
	// запускаем административный сервиса
	go func() {
		log.WithFields(log.Fields{
			"address": aserver.Addr,
		}).Info("starting admin server")
		err = aserver.ListenAndServe()
		if err != nil {
			log.WithError(err).Warning("admin server stoped")
		}
		os.Exit(3)
	}()

	// инициализируем мультиплексор HTTP-запросов
	var mux = &rest.ServeMux{
		Headers: map[string]string{
			"Server":            "Provisioning/1.0",
			"X-API-Version":     "1.0",
			"X-Service-Version": version,
		},
		Logger:  log.Default,
		Encoder: Encoder, // переопределяем формат вывода
	}
	// сборка единой конфигурации
	mux.Handle("GET", "/config", store.GetConfig)
	mux.Handle("GET", "/test", store.testMail)

	// инициализируем сервис для пользователей
	server := &http.Server{
		Addr:         ":https",
		Handler:      mux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 20,
	}

	host, port, err := net.SplitHostPort(host)
	if err != nil {
		log.WithError(err).Error("bad server address")
		os.Exit(2)
	}
	if host != "localhost" && host != "127.0.0.1" {
		manager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(host),
			Email:      "dmitrys@xyzrd.com",
			Cache:      autocert.DirCache("letsEncript.cache"),
		}
		server.TLSConfig = &tls.Config{
			GetCertificate: manager.GetCertificate,
		}
	} else {
		// исключительно для отладки
		cert, err := tls.X509KeyPair(LocalhostCert, LocalhostKey)
		if err != nil {
			panic(fmt.Sprintf("local certificates error: %v", err))
		}
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		server.Addr = net.JoinHostPort(host, port)
	}

	go func() {
		log.WithFields(log.Fields{
			"address": server.Addr,
			"host":    host,
		}).Info("starting https")
		err = server.ListenAndServeTLS("", "")
		// корректно закрываем сервисы по окончании работы
		if err != nil {
			log.WithError(err).Warning("https server stoped")
		}
		os.Exit(3)
	}()

	// инициализируем поддержку системных сигналов и ждем, когда он случится
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
