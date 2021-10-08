package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"phish-api/internal/elastic"
	"phish-api/internal/rabbitmq"
	"phish-api/internal/server"
	"phish-api/internal/validate"

	"github.com/streadway/amqp"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Http       server.HttpConfig        `yaml:"http"`
	Rabbit     rabbitmq.RabbitConfig    `yaml:"rabbit"`
	Validation validate.ValidatorConfig `yaml:"validation"`
	Elastic    elastic.ElasticConfig    `yaml:"elastic"`
}

func main() {
	var configPath string

	flag.StringVar(&configPath, "cfg", "../../configs/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := loadConfig(configPath)
	fatalOnErr(err)

	// rabbit
	rabbitHandler, err := rabbitmq.NewRabbitHandler(cfg.Rabbit)
	fatalOnErr(err)
	defer rabbitHandler.Close()

	// validator
	validator, err := validate.NewValidator(cfg.Validation)
	fatalOnErr(err)

	// elastic logger
	logger, err := elastic.NewElastic(cfg.Elastic)
	fatalOnErr(err)
	defer logger.Indexer.Close()

	// monitor sys and external events
	go monitorEvents(rabbitHandler.NewCloseCh())

	// server
	srv, err := server.NewServer(
		cfg.Http,
		rabbitHandler,
		validator,
		logger)
	fatalOnErr(err)

	// run server
	log.Fatal(srv.Up())
}

func loadConfig(path string) (*Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var cfg Config
	err = yaml.Unmarshal(bytes, &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file content: %v", err)
	}

	return &cfg, nil
}

func monitorEvents(closeCh <-chan *amqp.Error) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case sig := <-sigCh:
			log.Fatalf("catch signal (%v)-> stop", sig)

		case err := <-closeCh:
			log.Fatalf("amqp error (closeCh): %v", err)

		default:
			time.Sleep(time.Microsecond)
		}
	}
}

func fatalOnErr(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}
