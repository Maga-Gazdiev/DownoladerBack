package config

import "testing"

func TestLoadRabbitAMQPURL(t *testing.T) {
	const dsn = "amqps://user:pass@rabbit.example:5671/downloader"
	t.Setenv("RABBIT_AMQP_URL", dsn)
	t.Setenv("RABBITMQ_URL", "amqp://legacy:5672/")
	t.Setenv("RABBIT_HOST", "legacy-host")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RabbitURL != dsn {
		t.Fatalf("RabbitURL = %q, want %q", cfg.RabbitURL, dsn)
	}
}

func TestLoadDoesNotUseLegacyRabbitVariables(t *testing.T) {
	t.Setenv("RABBIT_AMQP_URL", "")
	t.Setenv("RABBITMQ_URL", "amqp://legacy:5672/")
	t.Setenv("RABBIT_HOST", "legacy-host")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RabbitURL != "" {
		t.Fatalf("RabbitURL = %q, want empty URL", cfg.RabbitURL)
	}
}
