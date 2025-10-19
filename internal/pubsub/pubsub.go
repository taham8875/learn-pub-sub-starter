package pubsub

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
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
	args := amqp.Table{
		"x-dead-letter-exchange": routing.ExchangePerilDeadLetter,
	}

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

// SubscribeJSON sets up a consumer to receive JSON messages from a queue
func SubscribeJSON[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType,
	handler func(T) AckType,
) error {
	return subscribe(conn, exchange, queueName, key, queueType, handler, func(data []byte) (T, error) {
		var message T
		err := json.Unmarshal(data, &message)
		return message, err
	})
}

func PublishGob[T any](ch *amqp.Channel, exchange, key string, val T) error {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	err := encoder.Encode(val)
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
			ContentType: "application/gob",
			Body:        buf.Bytes(),
		},
	)
}

// SubscribeGob sets up a consumer to receive gob-encoded messages from a queue
func SubscribeGob[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType,
	handler func(T) AckType,
) error {
	return subscribe(conn, exchange, queueName, key, queueType, handler, func(data []byte) (T, error) {
		var message T
		var buf bytes.Buffer
		buf.Write(data)
		decoder := gob.NewDecoder(&buf)
		err := decoder.Decode(&message)
		return message, err
	})
}

// Helper function to share duplicate code between SubscribeJSON and SubscribeGob
func subscribe[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	simpleQueueType SimpleQueueType,
	handler func(T) AckType,
	unmarshaller func([]byte) (T, error),
) error {
	ch, queue, err := DeclareAndBind(
		conn,
		exchange,
		queueName,
		key,
		simpleQueueType,
	)

	if err != nil {
		return err
	}

	err = ch.Qos(10, 0, false)
	if err != nil {
		ch.Close()
		return err
	}

	deliveries, err := ch.Consume(
		queue.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		ch.Close()
		return err
	}

	// start a goroutine to process deliveries
	go func() {
		defer ch.Close()
		for delivery := range deliveries {
			message, err := unmarshaller(delivery.Body)
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
		}
	}()

	return nil
}
