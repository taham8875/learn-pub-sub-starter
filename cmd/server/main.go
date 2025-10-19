package main

import (
	"fmt"
	// "os"
	// "os/signal"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func handleGameLog() func(routing.GameLog) pubsub.AckType {
	return func(log routing.GameLog) pubsub.AckType {
		defer fmt.Print("> ")

		err := gamelogic.WriteLog(log)
		if err != nil {
			fmt.Printf("Failed to write log to disk: %v\n", err)
			return pubsub.NackRequeue // Requeue on write failure
		}

		fmt.Printf("Log written to disk: %s\n", log.Message)
		return pubsub.Ack
	}
}

func main() {
	fmt.Println("Starting Peril server...")

	gamelogic.PrintServerHelp()

	connectionString := "amqp://guest:guest@localhost:5672/"

	conn, err := amqp.Dial(connectionString)
	if err != nil {
		fmt.Printf("Failed to connect to RabbitMQ: %v\n", err)
		return
	}

	defer conn.Close()

	ch, err := conn.Channel()

	if err != nil {
		fmt.Printf("Failed to open a channel: %v\n", err)
		return
	}

	defer ch.Close()

	pauseState := routing.PlayingState{
		IsPaused: true,
	}

	err = pubsub.PublishJSON(
		ch,
		routing.ExchangePerilDirect,
		routing.PauseKey,
		pauseState,
	)

	if err != nil {
		fmt.Printf("Failed to publish pause state: %v\n", err)
		return
	}

	fmt.Println("Published pause message successfully.")

	err = pubsub.SubscribeGob(
		conn,
		routing.ExchangePerilTopic,
		routing.GameLogSlug,
		routing.GameLogSlug+".*",
		pubsub.Durable,
		handleGameLog(),
	)

	if err != nil {
		fmt.Printf("Failed to subscribe to game logs: %v\n", err)
		return
	}
	fmt.Println("Subscribed to game logs.")

	for {
		words := gamelogic.GetInput()
		if len(words) == 0 {
			continue
		}

		command := words[0]

		switch command {
		case "pause":
			fmt.Println("Pausing the game...")
			pauseState := routing.PlayingState{
				IsPaused: true,
			}

			err = pubsub.PublishJSON(
				ch,
				string(routing.ExchangePerilDirect),
				string(routing.PauseKey),
				pauseState,
			)

			if err != nil {
				fmt.Printf("Failed to publish pause state: %v\n", err)
			} else {
				fmt.Println("Published pause message successfully.")
			}

		case "resume":
			fmt.Println("Resuming the game...")

			resumeState := routing.PlayingState{
				IsPaused: false,
			}

			err = pubsub.PublishJSON(
				ch,
				string(routing.ExchangePerilDirect),
				string(routing.PauseKey),
				resumeState,
			)

		case "quit":
			fmt.Println("Quitting the server...")
			return
		default:
			fmt.Println("Unknown command. Available commands: pause, resume, quit")
		}
	}

	// // wait for signal (any signal) to exit
	//
	// signalChan := make(chan os.Signal, 1)
	// signal.Notify(signalChan, os.Interrupt, os.Kill)
	//
	// // block until a signal is received
	// <-signalChan
	//
	// fmt.Println("Shutting down server...")
}
