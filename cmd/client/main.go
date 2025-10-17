package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	fmt.Println("Starting Peril client...")

	username, err := gamelogic.ClientWelcome()
	if err != nil {
		fmt.Printf("Error during welcome: %v\n", err)
		return
	}

	connectionString := "amqp://guest:guest@localhost:5672/"
	conn, err := amqp.Dial(connectionString)
	if err != nil {
		fmt.Printf("Failed to connect to RabbitMQ: %v\n", err)
		return
	}
	defer conn.Close()
	fmt.Printf("Connected to RabbitMQ as %s\n", username)

	queueName := routing.PauseKey + "." + username

	ch, queue, err := pubsub.DeclarAndBind(
		conn,
		routing.ExchangePerilDirect,
		queueName,
		routing.PauseKey,
		pubsub.Transient,
	)

	if err != nil {
		fmt.Printf("Failed to declare and bind queue: %v\n", err)
		return
	}

	defer ch.Close()

	fmt.Printf("Created transient queue %s and bound to exchange %s with routing key %s\n", queue.Name, routing.ExchangePerilDirect, routing.PauseKey)

	// Wait for signal to exit
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, os.Kill)

	// Block until signal is received
	<-signalChan

	fmt.Println("Shutting down client...")

}
