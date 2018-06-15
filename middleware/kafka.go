package middleware

import (
	"os"
	"os/signal"

	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"

	"github.com/labstack/echo"
	"github.com/shibingli/realclouds_go/utils"

	"crypto/tls"
	"fmt"
	"log"
	"strings"
	"time"
)

//Kafka *
type Kafka struct {
	BrokerList             []string
	SyncProducerCollector  sarama.SyncProducer
	AsyncProducerCollector sarama.AsyncProducer
}

//NewKafka *
func NewKafka(brokerList []string) (kafka *Kafka, err error) {

	if len(brokerList) == 0 {
		return nil, fmt.Errorf("%s", "Invalid broker data.")
	}

	kafka = &Kafka{
		BrokerList:             brokerList,
		SyncProducerCollector:  newSyncProducerCollector(brokerList),
		AsyncProducerCollector: newASyncProducerCollector(brokerList),
	}

	return kafka, nil
}

//Close *
func (k *Kafka) Close() error {
	if err := k.SyncProducerCollector.Close(); err != nil {
		log.Println("Failed to shut down sync producer collector cleanly", err)
	}

	if err := k.AsyncProducerCollector.Close(); err != nil {
		log.Println("Failed to shut down async producer collector cleanly", err)
	}
	return nil
}

//SyncSendMessage *
func (k *Kafka) SyncSendMessage(topic, msg string, key ...string) {
	topic = strings.TrimSpace(topic)
	msg = strings.TrimSpace(msg)

	producerMessage := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(msg),
	}

	if len(key) > 0 {
		producerMessage.Key = sarama.StringEncoder(key[0])
	}

	partition, offset, err := k.SyncProducerCollector.SendMessage(producerMessage)
	if err != nil {
		fmt.Printf("Failed to store your data:, %s\n", err)
	} else {
		fmt.Printf("Your data is stored with unique identifier important/%d/%d\n", partition, offset)
	}
}

//ASyncSendMessage *
func (k *Kafka) ASyncSendMessage(topic, msg string, key ...string) {
	topic = strings.TrimSpace(topic)
	msg = strings.TrimSpace(msg)

	producerMessage := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.StringEncoder(msg),
	}

	if len(key) > 0 {
		producerMessage.Key = sarama.StringEncoder(key[0])
	}

	k.AsyncProducerCollector.Input() <- producerMessage
}

func newSyncProducerCollector(brokerList []string) sarama.SyncProducer {

	config := sarama.NewConfig()

	tlsConfig := getKafakaTLSConfigByEnv()
	if tlsConfig != nil {
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = tlsConfig
	}

	config.Version = sarama.V0_10_2_1
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 10
	config.Producer.Return.Successes = true

	producer, err := sarama.NewSyncProducer(brokerList, config)
	if err != nil {
		log.Fatalln("Failed to start Sarama producer:", err)
	}

	return producer
}

func newASyncProducerCollector(brokerList []string) sarama.AsyncProducer {
	config := sarama.NewConfig()

	tlsConfig := getKafakaTLSConfigByEnv()
	if tlsConfig != nil {
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = tlsConfig
	}

	config.Version = sarama.V0_10_2_1
	config.Producer.RequiredAcks = sarama.WaitForLocal
	config.Producer.Compression = sarama.CompressionSnappy
	config.Producer.Flush.Frequency = 500 * time.Millisecond

	producer, err := sarama.NewAsyncProducer(brokerList, config)
	if err != nil {
		log.Fatalln("Failed to start Sarama producer:", err)
	}

	go func() {
		for err := range producer.Errors() {
			log.Println("Failed to write access log entry:", err)
		}
	}()

	return producer
}

func getKafakaTLSConfigByEnv() (t *tls.Config) {
	certFile := utils.GetENV("KAFKA_TLS_CERT")
	keyFile := utils.GetENV("KAFKA_TLS_KEY")
	caFile := utils.GetENV("KAFKA_TLS_CA")
	verifySSL := utils.GetENVToBool("KAFKA_TLS_VERIFYSSL")

	t = utils.CreateTLSConfig(certFile, keyFile, caFile, verifySSL)

	return
}

//MwKafaka Kafa middleware
func (k *Kafka) MwKafaka(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		c.Set("kafaka", k)
		return next(c)
	}
}

//Subscription *
func (k *Kafka) Subscription(topics []string, group string,
	onMessage func(topic string, partition int32, offset int64, key, value []byte) error) {

	config := cluster.NewConfig()
	config.Version = sarama.V0_10_2_1
	config.Consumer.Return.Errors = true
	config.Group.Return.Notifications = true

	consumer, err := cluster.NewConsumer(k.BrokerList, group, topics, config)
	if err != nil {
		log.Printf("Error: %s\n", err.Error())
	}
	defer consumer.Close()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill)

	go func() {
		for err := range consumer.Errors() {
			log.Printf("Error: %s\n", err.Error())
		}
	}()

	go func() {
		for ntf := range consumer.Notifications() {
			log.Printf("Rebalanced: %+v\n", ntf)
		}
	}()

	for {
		select {
		case msg, ok := <-consumer.Messages():
			if ok {
				if nil != onMessage {
					err := onMessage(msg.Topic, msg.Partition, msg.Offset, msg.Key, msg.Value)
					if err != nil {
						log.Printf("Error: %s\n", err.Error())
					} else {
						consumer.MarkOffset(msg, "")
					}
				}
			}
		case <-signals:
			break
		}
	}
}
