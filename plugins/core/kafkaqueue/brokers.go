package kafkaqueue

import (
	"os"
	"strings"

	"github.com/akarso/shopanda/internal/platform/config"
)

func ResolveBrokers(cfg config.KafkaQueueConfig) []string {
	brokers := append([]string(nil), cfg.Brokers...)
	if len(brokers) > 0 {
		return brokers
	}
	for _, envKey := range []string{"SHOPANDA_QUEUE_KAFKA_BROKERS", "KAFKA_BROKERS"} {
		if env := strings.TrimSpace(os.Getenv(envKey)); env != "" {
			for _, part := range strings.Split(env, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					brokers = append(brokers, part)
				}
			}
			if len(brokers) > 0 {
				return brokers
			}
		}
	}
	return nil
}
