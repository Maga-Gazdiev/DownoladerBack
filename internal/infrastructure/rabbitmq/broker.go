package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
)

type Broker struct {
	conn    *amqp.Connection
	closed  chan struct{}
	prefix  string
	retries int
}

func New(ctx context.Context, dsn, prefix string, retries int) (*Broker, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := amqp.DialConfig(dsn, amqp.Config{
		Heartbeat: 10 * time.Second,
		Dial: func(network, address string) (net.Conn, error) {
			c, err := dialer.DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			// Includes the AMQP handshake; heartbeats take over after DialConfig.
			if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
				c.Close()
				return nil, err
			}
			return &boundedConn{Conn: c}, nil
		},
	})
	if err != nil {
		return nil, errors.New("RabbitMQ connection failed; check address, credentials and vhost")
	}
	b := &Broker{conn: conn, closed: make(chan struct{}), prefix: prefix, retries: retries}
	if err := b.setup(); err != nil {
		conn.Close()
		return nil, err
	}
	notifications := conn.NotifyClose(make(chan *amqp.Error, 1))
	go func() {
		<-notifications
		close(b.closed)
	}()
	return b, nil
}

// PublishWithContext in amqp091-go does not cancel socket writes. Bound them too.
type boundedConn struct{ net.Conn }

func (c *boundedConn) Write(p []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
func (b *Broker) Close() error          { return b.conn.Close() }
func (b *Broker) Done() <-chan struct{} { return b.closed }
func (b *Broker) setup() error {
	ch, err := b.conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	for _, name := range []string{model.DownloadQueue, model.SendQueue, model.WebQueue} {
		name = b.prefix + name
		for _, q := range []struct {
			name string
			args amqp.Table
		}{
			{name + ".dead", nil},
			{name + ".retry", amqp.Table{"x-message-ttl": int32(10000), "x-dead-letter-exchange": "", "x-dead-letter-routing-key": name}},
			{name, amqp.Table{"x-dead-letter-exchange": "", "x-dead-letter-routing-key": name + ".dead"}},
		} {
			if _, err := ch.QueueDeclare(q.name, true, false, false, false, q.args); err != nil {
				return fmt.Errorf("declare %s: %w", q.name, err)
			}
		}
	}
	return nil
}
func (b *Broker) Publish(ctx context.Context, queue string, body []byte) error {
	return b.publish(ctx, b.prefix+queue, body, 0)
}
func (b *Broker) publish(ctx context.Context, queue string, body []byte, retry int32) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	ch, err := b.conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.Confirm(false); err != nil {
		return err
	}
	returned := ch.NotifyReturn(make(chan amqp.Return, 1))
	confirmation, err := ch.PublishWithDeferredConfirmWithContext(ctx, "", queue, true, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body,
		Headers: amqp.Table{"x-retry-count": retry},
	})
	if err != nil {
		return err
	}
	ok, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("RabbitMQ did not confirm publication")
	}
	select {
	case r := <-returned:
		return fmt.Errorf("RabbitMQ returned publication: %d", r.ReplyCode)
	default:
		return nil
	}
}

func (b *Broker) Consume(ctx context.Context, queue string, timeout time.Duration, process func(context.Context, []byte) error) error {
	queue = b.prefix + queue
	ch, err := b.conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.Qos(1, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("RabbitMQ consumer disconnected")
			}
			count, valid := retryCount(delivery.Headers)
			var processErr error
			if !valid {
				processErr = apperrors.Permanent(errors.New("invalid retry header"))
			} else {
				jobCtx, cancel := context.WithTimeout(ctx, timeout)
				processErr = process(jobCtx, delivery.Body)
				cancel()
			}
			// Closing the channel returns an interrupted delivery to the broker.
			if ctx.Err() != nil {
				return nil
			}
			if processErr != nil {
				slog.Warn("job failed", "queue", queue, "error", processErr)
			}
			err := settle(processErr, count, b.retries,
				func() error { return b.publish(ctx, queue+".retry", delivery.Body, int32(count+1)) },
				func() error { return delivery.Ack(false) },
				func() error { return delivery.Nack(false, false) },
			)
			if err != nil {
				return fmt.Errorf("settle delivery: %w", err)
			}
		}
	}
}

// Never acknowledge an original delivery before its retry is confirmed.
func settle(processErr error, count, max int, retry, ack, reject func() error) error {
	if processErr == nil {
		return ack()
	}
	if apperrors.IsPermanent(processErr) || count >= max {
		return reject()
	}
	if err := retry(); err != nil {
		return err
	}
	return ack()
}
func retryCount(headers amqp.Table) (int, bool) {
	v, ok := headers["x-retry-count"]
	if !ok {
		return 0, true
	}
	n, ok := v.(int32)
	return int(n), ok && n >= 0
}
