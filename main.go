package main

import (
	"context"
	"fmt"
	"github.com/jaegertracing/jaeger/model"
	"github.com/jaegertracing/jaeger/proto-gen/api_v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	tele "gopkg.in/telebot.v3"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	checkInterval := time.Duration(getRequiredIntegerEnvironmentVariable("CHECK_INTERVAL_MINUTES")) * time.Minute
	maximumAge := time.Duration(getRequiredIntegerEnvironmentVariable("MAXIMUM_AGE_HOURS")) * time.Hour
	jaegerAddr := getRequiredEnvironmentVariable("JAEGER_ADDR")
	serviceName := getRequiredEnvironmentVariable("JAEGER_SERVICE_NAME")
	recipientChatId := tele.ChatID(getRequiredIntegerEnvironmentVariable("TELEGRAM_RECIPIENT_USER_ID"))
	botToken := getRequiredEnvironmentVariable("TELEGRAM_BOT_TOKEN")

	bot, err := tele.NewBot(tele.Settings{Token: botToken})
	if err != nil {
		log.Fatal(err)
	}

	lastStartTime := time.Now().Add(-maximumAge)
	for {
		nextStartTime := time.Now()
		errors, err := getErrorMessages(err, jaegerAddr, lastStartTime, serviceName)
		if err != nil {
			log.Println(err)
		} else {
			if len(errors) > 0 {
				err := sendErrorMessages(errors, bot, recipientChatId)
				if err != nil {
					log.Println(err)
				}
			}
			lastStartTime = nextStartTime
		}
		if lastStartTime.Before(time.Now().Add(-maximumAge)) {
			lastStartTime = time.Now().Add(-maximumAge)
		}
		time.Sleep(checkInterval)
	}
}

func getRequiredEnvironmentVariable(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatalf("Required environment variable %s not set", key)
	}
	return value
}

func getRequiredIntegerEnvironmentVariable(key string) int64 {
	value := getRequiredEnvironmentVariable(key)
	intValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		log.Fatalf("Required environment variable %s not set", key)
	}
	return intValue
}

func sendErrorMessages(errorMessages []string, bot *tele.Bot, recipient tele.ChatID) error {
	_, err := bot.Send(recipient, fmt.Sprintf("Found %d errors", len(errorMessages)))
	if err != nil {
		return err
	}
	for _, errorMessage := range errorMessages {
		fmt.Println(errorMessage)
		fmt.Println()
		if len(errorMessage) > 4000 {
			errorMessage = errorMessage[:4000]
		}
		_, err := bot.Send(recipient, errorMessage)
		if err != nil {
			return err
		}
	}
	return nil
}

func getErrorMessages(err error, serverAddr string, startTime time.Time, serviceName string) ([]string, error) {
	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			log.Println(err)
		}
	}(conn)
	client := api_v2.NewQueryServiceClient(conn)
	result, err := client.FindTraces(
		context.Background(),
		&api_v2.FindTracesRequest{
			Query: &api_v2.TraceQueryParameters{
				ServiceName:  serviceName,
				Tags:         map[string]string{"error": "true"},
				StartTimeMin: startTime,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	var errors []string
	for {
		res, err := result.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors, err
		}
		for _, span := range res.GetSpans() {
			for _, spanLog := range span.GetLogs() {
				isError := false
				for _, field := range spanLog.GetFields() {
					if field.GetKey() == "level" && strings.ToLower(field.GetVStr()) == "error" {
						isError = true
						break
					}
					field.GetVStr()
				}
				if isError {
					errors = append(errors, createErrorMessage(span, spanLog))
				}
			}
		}
	}
	return errors, nil
}

func createErrorMessage(span model.Span, log model.Log) string {
	operation := span.GetOperationName()
	namespace := ""
	traceId := span.TraceID.String()
	event := ""
	target := ""
	message := ""

	for _, tag := range span.GetTags() {
		if tag.GetKey() == "code.namespace" {
			namespace = tag.GetVStr()
		}
	}
	for _, log := range log.GetFields() {
		if log.GetKey() == "event" {
			event = log.GetVStr()
		}
		if log.GetKey() == "target" {
			target = log.GetVStr()
		}
		if log.GetKey() == "exception.message" {
			message = log.GetVStr()
		}
	}

	return fmt.Sprintf("Error occured!\n\nParent: %s (%s)\nTrace: %s\n\nTarget: %s\n\n%s\n%s", operation, namespace, traceId, target, event, message)
}
