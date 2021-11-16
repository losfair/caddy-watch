package interest

import (
	"fmt"
	"strings"
	"time"

	"github.com/Jeffail/gabs/v2"
	"github.com/ReneKroon/ttlcache/v2"
	"github.com/confluentinc/confluent-kafka-go/kafka"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type InterestDef struct {
	Name                 string
	Description          string
	Disabled             bool `yaml:",omitempty"`
	Matcher              InterestMatcher
	SuppressDurationSecs uint64 `yaml:"suppressDurationSecs,omitempty"`
}

type InterestMatcher struct {
	All    []*InterestMatcher
	Any    []*InterestMatcher
	Status *InterestMatcher_Status
	Host   *string
	Method *string
}

type InterestMatcher_Status struct {
	Min uint16
	Max uint16
}

type interestMatchContext struct {
	logger *zap.Logger
	j      *gabs.Container
}

type interestSuppressionKey struct {
	remoteIp string
	host     string
}

func (k interestSuppressionKey) String() string {
	return k.remoteIp + "/" + k.host
}

func (ctx *interestMatchContext) match(m *InterestMatcher) bool {
	for _, subM := range m.All {
		if !ctx.match(subM) {
			return false
		}
	}

	anyMatch := false
	for _, subM := range m.Any {
		if ctx.match(subM) {
			anyMatch = true
			break
		}
	}
	if len(m.Any) != 0 && !anyMatch {
		return false
	}

	if m.Status != nil {
		jStatus_, ok := ctx.j.Path("status").Data().(float64)
		if !ok {
			return false
		}
		jStatus := uint16(jStatus_)
		if jStatus < m.Status.Min || jStatus > m.Status.Max {
			return false
		}
	}

	if m.Host != nil {
		jHost, ok := ctx.j.Path("request.host").Data().(string)
		if !ok {
			return false
		}
		if *m.Host != jHost {
			return false
		}
	}

	if m.Method != nil {
		jMethod, ok := ctx.j.Path("request.method").Data().(string)
		if !ok {
			return false
		}
		if *m.Method != jMethod {
			return false
		}
	}

	return true
}

type InterestDispatcher struct {
	logger        *zap.Logger
	interests     []InterestDef
	suppressCache *ttlcache.Cache
	botApi        *tgbotapi.BotAPI
	toChat        int64
}

func NewInterestDispatcher(logger *zap.Logger, interests []InterestDef, botToken string, toChat int64) (*InterestDispatcher, error) {
	logger.Info("connecting to telegram bot api")
	botApi, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, errors.Wrap(err, "cannot connect to telegram bot api")
	}
	logger.Info("telegram authentication succeeded", zap.String("user", botApi.Self.UserName))

	return &InterestDispatcher{
		logger:        logger,
		interests:     interests,
		suppressCache: ttlcache.NewCache(),
		botApi:        botApi,
		toChat:        toChat,
	}, nil
}

func (d *InterestDispatcher) Close() {
	d.suppressCache.Close()
}

func (d *InterestDispatcher) Dispatch(message *kafka.Message) {
	j, err := gabs.ParseJSON(message.Value)
	if err != nil {
		d.logger.Error("cannot parse json", zap.Error(err), zap.String("message", string(message.Value)))
		return
	}

	for i, interest := range d.interests {
		if interest.Disabled {
			continue
		}

		ctx := interestMatchContext{
			logger: d.logger.With(zap.String("interest_name", interest.Name), zap.Int("interest_index", i)),
			j:      j,
		}
		if ctx.match(&interest.Matcher) {
			if interest.SuppressDurationSecs > 0 {
				remoteAddr, _ := j.Path("request.remote_addr").Data().(string)
				host, _ := j.Path("request.host").Data().(string)
				remoteIp := strings.Split(remoteAddr, ":")[0]

				key := interestSuppressionKey{
					remoteIp: remoteIp,
					host:     host,
				}.String()
				if _, err := d.suppressCache.Get(key); err == nil {
					continue
				}
				d.suppressCache.SetWithTTL(key, struct{}{}, time.Second*time.Duration(interest.SuppressDurationSecs))
			}

			d.sendToTelegram(&interest, j)
		}
	}
}

func (d *InterestDispatcher) sendToTelegram(interest *InterestDef, j *gabs.Container) {
	ts, _ := j.Path("ts").Data().(float64)
	parseMode := tgbotapi.ModeMarkdown

	d.logger.Info("sending to telegram", zap.String("interest_name", interest.Name))
	body := "```" + `
Interest: ` + tgbotapi.EscapeText(parseMode, interest.Name) + `
Description: ` + tgbotapi.EscapeText(parseMode, interest.Description) + `
Timestamp: ` + fmt.Sprintf("%f", ts) + `
Time: ` + time.Unix(int64(ts), 0).Format(time.RFC3339) + `
Host: ` + tgbotapi.EscapeText(parseMode, j.Path("request.host").String()) + `
Method: ` + tgbotapi.EscapeText(parseMode, j.Path("request.method").String()) + `
Status: ` + tgbotapi.EscapeText(parseMode, j.Path("status").String()) + `
URI: ` + tgbotapi.EscapeText(parseMode, j.Path("request.uri").String()) + `
Remote: ` + tgbotapi.EscapeText(parseMode, j.Path("request.remote_addr").String()) + `
` + "```"

	msg := tgbotapi.NewMessage(d.toChat, body)
	msg.ParseMode = parseMode
	_, err := d.botApi.Send(msg)
	if err != nil {
		d.logger.Error("cannot send message to telegram", zap.Error(err))
	} else {
		d.logger.Info("message sent to telegram")
	}
}
