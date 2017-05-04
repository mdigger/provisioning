package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/mdigger/log"
	"github.com/mdigger/rest"
)

var (
	appName = "provisioning"   // название сервиса
	version = "0.2.0"          // версия
	date    = "2017-05-04"     // дата сборки
	build   = ""               // номер сборки в git-репозитории
	host    = "localhost:8080" // адрес сервера и порт
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
	flag.StringVar(&host, "address", host, "server address and `port`")
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
	var mux = &rest.ServeMux{
		Headers: map[string]string{
			"Server":            "Provisioning/1.0",
			"X-API-Version":     "1.0",
			"X-Service-Version": version,
		},
		Logger:  log.Default,
		Encoder: Encoder, // переопределяем формат вывода
	}
	// задаем обработчики запросов
	mux.Handles(rest.Paths{
		// обработчики администраторов
		"/admin/auth": rest.Methods{
			"GET":  store.List(bucketAdmins),
			"POST": store.Post(bucketAdmins),
		},
		"/admin/auth/:name": rest.Methods{
			"GET":    store.Get(bucketAdmins),
			"PUT":    store.Put(bucketAdmins),
			"DELETE": store.Remove(bucketAdmins),
		},
		// обработчики сервисов
		"/admin/services": rest.Methods{
			"GET":  store.List(bucketServices),
			"POST": store.Post(bucketServices),
		},
		"/admin/services/:name": rest.Methods{
			"GET":    store.Get(bucketServices),
			"PUT":    store.Put(bucketServices),
			"DELETE": store.Remove(bucketServices),
		},
		// обработчики групп пользователей
		"/admin/groups": rest.Methods{
			"GET":  store.List(bucketGroups),
			"POST": store.Post(bucketGroups),
		},
		"/admin/groups/:name": rest.Methods{
			"GET":    store.Get(bucketGroups),
			"PUT":    store.Put(bucketGroups),
			"DELETE": store.Remove(bucketGroups),
		},
		// обработчики пользователей
		"/admin/users": rest.Methods{
			"GET":  store.List(bucketUsers),
			"POST": store.Post(bucketUsers),
		},
		"/admin/users/:name": rest.Methods{
			"GET":    store.Get(bucketUsers),
			"PUT":    store.Put(bucketUsers),
			"DELETE": store.Remove(bucketUsers),
		},
	}, store.CheckAdmins)

	mux.Handle("GET", "/backup", store.Backup)
	mux.Handle("GET", "/config", store.GetConfig)

	// инициализируем HTTP-сервер
	server := &http.Server{
		Addr:         host,
		Handler:      mux,
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 20,
	}
	// запускаем сервер
	go func() {
		log.WithField("address", server.Addr).Info("starting http")
		if err := server.ListenAndServe(); err != nil {
			log.WithError(err).Warning("http server stoped")
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
