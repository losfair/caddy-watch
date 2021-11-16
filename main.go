package main

import (
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/losfair/caddy-watch/interest"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

type ConfigDef struct {
	Interests []interest.InterestDef
	Telegram  TelegramConfig
}

type TelegramConfig struct {
	BotToken string `yaml:"botToken"`
	ToChat   int64  `yaml:"toChat"`
}

func buildInterestDispatcher(logger *zap.Logger, configPath string) (*interest.InterestDispatcher, error) {
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var configDef ConfigDef

	err = yaml.Unmarshal(configBytes, &configDef)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse config")
	}

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		logger.Warn("TELEGRAM_BOT_TOKEN is not set, falling back to `telegram.botToken`")
		botToken = configDef.Telegram.BotToken
	} else {
		logger.Info("using environment variable TELEGRAM_BOT_TOKEN")
	}

	return interest.NewInterestDispatcher(
		logger.With(zap.String("component", "interest-dispatcher")),
		configDef.Interests,
		botToken,
		configDef.Telegram.ToChat,
	)
}

func main() {
	bootstrapServers := flag.String("bootstrap-servers", "localhost:9092", "Comma separated list of Kafka brokers")
	groupId := flag.String("group-id", "com.example.caddy-watch.default", "Kafka consumer group id")
	topics := flag.String("topics", "com.example.caddy.default", "Comma separated list of Kafka topics to consume")
	config := flag.String("config", "./caddy-watch.yaml", "Path to the config file")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	interestDispatcher, err := buildInterestDispatcher(logger, *config)
	if err != nil {
		logger.Panic("cannot build interest dispatcher", zap.Error(err))
	}
	defer interestDispatcher.Close()

	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	hupchan := make(chan os.Signal, 1)
	signal.Notify(hupchan, syscall.SIGHUP)

	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":        *bootstrapServers,
		"group.id":                 *groupId,
		"auto.offset.reset":        "latest",
		"enable.auto.offset.store": false,
	})
	if err != nil {
		logger.Panic("kafka consumer init failed", zap.Error(err))
	}
	defer c.Close()

	topicList := strings.Split(*topics, ",")
	err = c.SubscribeTopics(topicList, nil)
	for {
		select {
		case sig := <-sigchan:
			logger.Info("signal received", zap.String("signal", sig.String()))
			return
		case sig := <-hupchan:
			logger.Info("requested config reload", zap.String("signal", sig.String()))
			newInterestDispatcher, err := buildInterestDispatcher(logger, *config)
			if err != nil {
				logger.Error("cannot build new interest dispatcher", zap.Error(err))
				continue
			}
			newInterestDispatcher.SwapState(interestDispatcher)
			interestDispatcher.Close()
			interestDispatcher = newInterestDispatcher
			logger.Info("config reload succeeded")
		default:
			ev := c.Poll(1000)
			if ev == nil {
				continue
			}

			switch e := ev.(type) {
			case *kafka.Message:
				//logger.Info("received message", zap.String("topic", *e.TopicPartition.Topic), zap.String("value", string(e.Value)))
				interestDispatcher.Dispatch(e)
				_, err = c.StoreOffsets([]kafka.TopicPartition{e.TopicPartition})

				// Something very bad happended - don't silently poll and process the same message twice.
				if err != nil {
					logger.Panic("cannot store offsets", zap.Error(err))
				}
			case kafka.Error:
				logger.Error("kafka error", zap.Error(e))
			case kafka.OffsetsCommitted:
			default:
				logger.Warn("unexpected event", zap.Any("event", e))
			}
		}
	}
}
