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

func handlePause(gs *gamelogic.GameState) func(routing.PlayingState) pubsub.AckType {
	return func(ps routing.PlayingState) pubsub.AckType {
		defer fmt.Print("> ")
		gs.HandlePause(ps)
		return pubsub.Ack
	}
}

func handleArmyMove(gs *gamelogic.GameState) func(gamelogic.ArmyMove) pubsub.AckType {
	return func(move gamelogic.ArmyMove) pubsub.AckType {
		defer fmt.Print("> ")
		outcome := gs.HandleMove(move)

		// determine ack based on the outcome of the move
		switch outcome {
		case gamelogic.MoveOutComeSafe:
			return pubsub.Ack
		case gamelogic.MoveOutcomeMakeWar:
			return pubsub.Ack
		case gamelogic.MoveOutcomeSamePlayer:
			return pubsub.NackDiscard
		default:
			return pubsub.NackDiscard
		}
	}
}

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

	gameState := gamelogic.NewGameState(username)
	fmt.Printf("Game state created for user: %s\n", gameState.GetUsername())

	queueName := routing.PauseKey + "." + username

	err = pubsub.SubscribeJSON(
		conn,
		routing.ExchangePerilDirect,
		queueName,
		routing.PauseKey,
		pubsub.Transient,
		handlePause(gameState),
	)

	if err != nil {
		fmt.Printf("Failed to subscribe to pause messages: %v\n", err)
		return
	}

	// Subscribe to army moves from other players
	armyMoveQueueName := routing.ArmyMovesPrefix + "." + username
	armyMoveRoutingKey := routing.ArmyMovesPrefix + ".*"

	err = pubsub.SubscribeJSON(
		conn,
		routing.ExchangePerilTopic,
		armyMoveQueueName,
		armyMoveRoutingKey,
		pubsub.Transient,
		handleArmyMove(gameState),
	)

	if err != nil {
		fmt.Printf("Failed to subscribe to army move messages: %v\n", err)
		return
	}

	for {
		words := gamelogic.GetInput()

		if len(words) == 0 {
			continue
		}

		command := words[0]

		switch command {
		case "spawn":
			err := gameState.CommandSpawn(words)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		case "move":
			armyMove, err := gameState.CommandMove(words)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			moveRoutingKey := routing.ArmyMovesPrefix + "." + username

			ch, err := conn.Channel()
			if err != nil {
				fmt.Printf("Failed to open channel: %v\n", err)
				continue
			}

			err = pubsub.PublishJSON(
				ch,
				routing.ExchangePerilTopic,
				moveRoutingKey,
				armyMove,
			)

			ch.Close()
			if err != nil {
				fmt.Printf("Failed to publish army move: %v\n", err)
			} else {
				fmt.Printf("Army move published successfully to routing key %s\n", moveRoutingKey)
			}
		case "status":
			gameState.CommandStatus()
		case "help":
			gamelogic.PrintClientHelp()
		case "spam":
			fmt.Printf("Spamming not allowed yet!\n")
		case "quit":
			gamelogic.PrintQuit()
		default:
			fmt.Printf("Unknown command: %s\n", command)
			fmt.Println("Enter command (spawn, move, status, help, spam, quit): ")
		}
	}

	// fmt.Printf("Created transient queue %s and bound to exchange %s with routing key %s\n", queue.Name, routing.ExchangePerilDirect, routing.PauseKey)
	//
	// // Wait for signal to exit
	// signalChan := make(chan os.Signal, 1)
	// signal.Notify(signalChan, os.Interrupt, os.Kill)
	//
	// // Block until signal is received
	// <-signalChan
	//
	// fmt.Println("Shutting down client...")
}
