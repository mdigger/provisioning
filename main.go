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
	version = "0.1.2"          // версия
	date    = "2017-04-29"     // дата сборки
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

	// инициализируем поддержку разделов хранилища
	sections := &Sections{store: store}
	admins := &Admins{store: store}
	mux.Handles(rest.Paths{
		// административные права и пользователи
		"/admin/auth": rest.Methods{
			"GET":  admins.List,
			"POST": admins.Post,
		},
		"/admin/auth/:key": rest.Methods{
			"GET":    admins.Get,
			"PUT":    admins.Put,
			"DELETE": admins.Delete,
		},

		// остальные разделы хранилища данных
		"/admin/:section": rest.Methods{
			"GET":  sections.List,
			"POST": sections.Post,
		},
		"/admin/:section/:key": rest.Methods{
			"GET":    sections.Get,
			"PUT":    sections.Put,
			"DELETE": sections.Delete,
		},
	}, admins.Check)

	// инициализируем обработчик конфигурации пользователя
	users := &Users{store: store}
	mux.Handle("GET", "/users/:name", users.Get)

	mux.Handle("GET", "/backup", store.Backup)

	// инициализируем HTTP-сервер
	server := &http.Server{
		Addr:         "localhost:8080", // TODO: переопределить на имя
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
