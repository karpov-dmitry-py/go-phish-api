package rabbitmq

import (
	"errors"
	"log"
	"time"

	"github.com/streadway/amqp"
)

type RabbitChannel struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

type RabbitConfig struct {
	Dst struct {
		Dsn       string            `yaml:"dsn"`
		Exchange  string            `yaml:"exchange"`
		Exchanges map[string]string `yaml:"exchanges"`
	} `yaml:"dst"`
}

func (cfg *RabbitConfig) IsValid() bool {
	valid := true
	cfgName := "dst rabbit"

	// dst rabbit
	dstRabbit := cfg.Dst

	if dstRabbit.Dsn == "" {
		valid = false
		log.Printf("%v dsn is invalid", cfgName)
	}

	if dstRabbit.Exchange == "" {
		valid = false
		log.Printf("%v exchange is invalid", cfgName)
	}

	if len(dstRabbit.Exchanges) == 0 {
		valid = false
		log.Printf("%v exchange list is empty", cfgName)
	}

	for key, val := range dstRabbit.Exchanges {
		if key == "" || val == "" {
			valid = false
			log.Printf("%v exchange list is invalid", cfgName)
			break
		}
	}
	return valid
}

type RabbitHandler struct {
	ProdCh         *RabbitChannel
	MainExchange   string
	ExtraExchanges map[string]string
}

func NewRabbitHandler(cfg RabbitConfig) (*RabbitHandler, error) {
	if !cfg.IsValid() {
		return nil, errors.New("rabbit cfg is invalid")
	}

	prodCh := newChannel(cfg.Dst.Dsn)
	handler := &RabbitHandler{
		ProdCh:         prodCh,
		MainExchange:   cfg.Dst.Exchange,
		ExtraExchanges: cfg.Dst.Exchanges,
	}
	return handler, nil
}

func (h *RabbitHandler) NewCloseCh() <-chan *amqp.Error {
	return h.ProdCh.NotifyClose()
}

func (h *RabbitHandler) Close() {
	h.ProdCh.Close()
}

func (h *RabbitHandler) Publish(taskSource, routingKey string, message []byte) {
	// push to particular exchange based on task source
	exchange := h.MainExchange
	exch, found := h.ExtraExchanges[taskSource]
	if found {
		exchange = exch
	}

	err := h.ProdCh.Publish(exchange, routingKey, message)
	if err != nil {
		log.Fatalf("failed to publish a message to rabbit, err: %v", err)
	}
}

// RabbitChannel is a rabbitmq channel instance, used for consume & publish
func newChannel(dsn string) *RabbitChannel {
	heartbeat := time.Duration(time.Second * 600)
	rc := &RabbitChannel{}
	var err error
	rc.conn, err = amqp.DialConfig(dsn, amqp.Config{
		Heartbeat: heartbeat,
	})

	if err != nil {
		log.Fatalf("failed to connect to rabbitmq, err: %s", err)
	}

	rc.ch, err = rc.conn.Channel()
	if err != nil {
		log.Fatalf("failed to open a rabbit channel, err: %s", err)
	}

	return rc
}

// NewProducer creates new Producer instance
func NewProducer(dsn string) *RabbitChannel {
	return newChannel(dsn)
}

// NewConsumer creates new Consumer instance
func NewConsumer(dsn string, prefetch int) *RabbitChannel {
	consumer := newChannel(dsn)
	err := consumer.ch.Qos(prefetch, 0, false)
	if err != nil {
		log.Fatalf("Qos failed, err: %s", err)
	}
	return consumer
}

// Close gracefully closes rabbitmq channel and connection
func (rc *RabbitChannel) Close() {
	rc.ch.Close()
	rc.conn.Close()
}

// NotifyClose make error channel for signalling a transport or protocol error
func (rc *RabbitChannel) NotifyClose() <-chan *amqp.Error {
	return rc.conn.NotifyClose(make(chan *amqp.Error))
}

// Consume return channel for consuming messages from rabbitmq
func (rc *RabbitChannel) Consume(queue string) <-chan amqp.Delivery {
	deliveryChan, err := rc.ch.Consume(
		queue, // queue
		"",    // consumer
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		log.Fatalf("failed to consume from rabbit queue %s, err: %s", queue, err)
	}

	return deliveryChan
}

// Publish message to rabbitmq channel
func (rc *RabbitChannel) Publish(exchange, routingKey string, message []byte) error {
	err := rc.ch.Publish(
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         message,
		})
	if err != nil {
		return err
	}
	return nil
}
