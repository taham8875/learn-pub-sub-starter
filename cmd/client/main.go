package main

import (
	"fmt"
	"time"
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

func handleArmyMove(gs *gamelogic.GameState, conn *amqp.Connection) func(gamelogic.ArmyMove) pubsub.AckType {
	return func(move gamelogic.ArmyMove) pubsub.AckType {
		defer fmt.Print("> ")
		outcome := gs.HandleMove(move)

		// if ware is decalred, publish war message
		if outcome == gamelogic.MoveOutcomeMakeWar {
			warMessage := gamelogic.RecognitionOfWar{
				Attacker: move.Player,
				Defender: gs.GetPlayerSnap(),
			}

			warRoutingKey := routing.WarRecognitionsPrefix + "." + gs.GetUsername()

			fmt.Printf("Publishing war recognition message %v to routing key %s\n", warMessage, warRoutingKey)

			ch, err := conn.Channel()

			if err != nil {
				fmt.Printf("Failed to create channel for war message: %v\n", err)
				return pubsub.NackRequeue // Requeue on channel creation failure
			}

			err = pubsub.PublishJSON(ch, routing.ExchangePerilTopic, warRoutingKey, warMessage)
			ch.Close()

			if err != nil {
				fmt.Printf("Failed to publish war recognition message: %v\n", err)
				return pubsub.NackRequeue // Requeue on publish failure
			} else {
				fmt.Printf("War recognition message published successfully to routing key %s\n", warRoutingKey)
				return pubsub.Ack // Successfully published war message, acknowledge the move
			}
		}

		// determine ack based on the outcome of the move
		switch outcome {
		case gamelogic.MoveOutComeSafe:
			return pubsub.Ack
		case gamelogic.MoveOutcomeMakeWar:
			return pubsub.NackRequeue // This will cause requeue hell!
		case gamelogic.MoveOutcomeSamePlayer:
			return pubsub.NackDiscard
		default:
			return pubsub.NackDiscard
		}
	}
}

func handleWar(gs *gamelogic.GameState, conn *amqp.Connection) func(gamelogic.RecognitionOfWar) pubsub.AckType {
	return func(war gamelogic.RecognitionOfWar) pubsub.AckType {
		defer fmt.Print("> ")

		outcome, winner, loser := gs.HandleWar(war)

		err := publishGameLog(conn, gs, war, outcome, winner, loser)
		if err != nil {
			fmt.Printf("Failed to publish game log: %v\n", err)
			return pubsub.NackRequeue
		}

		switch outcome {
		case gamelogic.WarOutcomeNotInvolved:
			return pubsub.NackRequeue // Let another client handle it
		case gamelogic.WarOutcomeNoUnits:
			return pubsub.NackDiscard // Invalid war, discard
		case gamelogic.WarOutcomeOpponentWon:
			return pubsub.Ack // War resolved, acknowledge
		case gamelogic.WarOutcomeYouWon:
			return pubsub.Ack // War resolved, acknowledge
		case gamelogic.WarOutcomeDraw:
			return pubsub.Ack // War resolved, acknowledge
		default:
			fmt.Printf("Unknown war outcome: %v\n", outcome)
			return pubsub.NackDiscard
		}

	}
}

func publishGameLog(conn *amqp.Connection, gs *gamelogic.GameState, war gamelogic.RecognitionOfWar, outcome gamelogic.WarOutcome, winner string, loser string) error {
	var message string

	switch outcome {
	case gamelogic.WarOutcomeOpponentWon, gamelogic.WarOutcomeYouWon:
		message = fmt.Sprintf("%s won a war against %s", winner, loser)
	case gamelogic.WarOutcomeDraw:
		message = fmt.Sprintf("A war between %s and %s resulted in a draw", winner, loser)
	default:
		// Don't log wars that aren't resolved or aren't involved
		return nil
	}

	gameLog := routing.GameLog{
		CurrentTime: time.Now(),
		Message:     message,
		Username:    war.Attacker.Username,
	}

	ch, err := conn.Channel()

	if err != nil {
		return err
	}

	defer ch.Close()

	routingKey := routing.GameLogSlug + "." + war.Attacker.Username

	err = pubsub.PublishGob(ch, routing.ExchangePerilTopic, routingKey, gameLog)
	if err != nil {
		return err
	}

	fmt.Printf("Published game log: %s\n", message)
	return nil
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
		handleArmyMove(gameState, conn),
	)

	if err != nil {
		fmt.Printf("Failed to subscribe to army move messages: %v\n", err)
		return
	}

	fmt.Printf("Subscribed to army moves with queue %s and routing key %s\n", armyMoveQueueName, armyMoveRoutingKey)

	warMoveQueueName := "war"
	warMoveRoutingKey := routing.WarRecognitionsPrefix + ".*"

	err = pubsub.SubscribeJSON(
		conn,
		routing.ExchangePerilTopic,
		warMoveQueueName,
		warMoveRoutingKey,
		pubsub.Durable,
		handleWar(gameState, conn),
	)

	if err != nil {
		fmt.Printf("Failed to subscribe to war recognition messages: %v\n", err)
		return
	}

	fmt.Printf("Subscribed to war recognitions with queue %s and routing key %s\n", warMoveQueueName, warMoveRoutingKey)

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
