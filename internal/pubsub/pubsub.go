package pubsub

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// represents the type of queue either durable or transient
type SimpleQueueType int

type AckType int

const (
	Ack AckType = iota
	NackRequeue
	NackDiscard
)

const (
	Durable SimpleQueueType = iota
	Transient
)

func PublishJSON[T any](ch *amqp.Channel, exchange, key string, val T) error {
	jsonBytes, err := json.Marshal(val)

	if err != nil {
		return err
	}

	return ch.PublishWithContext(
		context.Background(),
		exchange,
		key,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        jsonBytes,
		},
	)
}

func DeclareAndBind(
	conn *amqp.Connection,
	exchange, queueName, key string,
	queueType SimpleQueueType,
) (*amqp.Channel, amqp.Queue, error) {

	// create new channel
	ch, err := conn.Channel()
	if err != nil {
		return nil, amqp.Queue{}, err
	}

	// declare queue
	var durable, autoDelete, exclusive bool
	var noWait bool = false
	var args amqp.Table = nil

	switch queueType {
	case Durable:
		durable = true
		autoDelete = false
		exclusive = false
	case Transient:
		durable = false
		autoDelete = true
		exclusive = true
	}

	queue, err := ch.QueueDeclare(
		queueName,
		durable,
		autoDelete,
		exclusive,
		noWait,
		args,
	)

	if err != nil {
		ch.Close()
		return nil, amqp.Queue{}, err
	}

	err = ch.QueueBind(
		queueName,
		key,
		exchange,
		noWait,
		args,
	)

	if err != nil {
		ch.Close()
		return nil, amqp.Queue{}, err
	}

	return ch, queue, nil
}

func SubscribeJSON[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType,
	handler func(T) AckType,
) error {
	ch, queue, err := DeclareAndBind(
		conn,
		exchange,
		queueName,
		key,
		queueType,
	)

	if err != nil {
		return err
	}

	deliveries, err := ch.Consume(
		queue.Name, // queueName
		"",         // consumer (empty string means auto-generate a name)
		false,      // autoAck
		false,      // exclusive
		false,      // noLocal
		false,      // noWait
		nil,        // args
	)

	if err != nil {
		ch.Close()
		return err
	}

	// start a goroutine to process deliveries
	go func() {
		defer ch.Close()
		for delivery := range deliveries {
			var message T
			err := json.Unmarshal(delivery.Body, &message)
			if err != nil {
				fmt.Println("Error unmarshaling message:", err)
				delivery.Nack(false, false)
				continue
			}

			ackType := handler(message)

			switch ackType {
			case Ack:
				delivery.Ack(false)
				fmt.Println("Message Acknowledged")
			case NackRequeue:
				delivery.Nack(false, true)
				fmt.Println("Message Not Acknowledged - Requeued")
			case NackDiscard:
				delivery.Nack(false, false)
				fmt.Println("Message Not Acknowledged - Discarded")
			}

			delivery.Ack(false)
		}
	}()

	return nil

}
