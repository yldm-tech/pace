package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const magicLinkTaskName = "plane.bgtasks.magic_link_code_task.magic_link"
const forgotPasswordTaskName = "plane.bgtasks.forgot_password_task.forgot_password"
const userActivationTaskName = "plane.bgtasks.user_activation_email_task.user_activation_email"
const emailUpdateCodeTaskName = "plane.bgtasks.user_email_update_task.send_email_update_magic_code"
const emailUpdateConfirmationTaskName = "plane.bgtasks.user_email_update_task.send_email_update_confirmation"
const userDeactivationTaskName = "plane.bgtasks.user_deactivation_email_task.user_deactivation_email"

type CeleryPublisher struct{ brokerURL string }

func NewCeleryPublisher(brokerURL string) *CeleryPublisher {
	return &CeleryPublisher{brokerURL: brokerURL}
}

func (publisher *CeleryPublisher) PublishMagicLink(ctx context.Context, email, key, token string) error {
	return publisher.publish(ctx, magicLinkTaskName, []any{email, key, token})
}

func (publisher *CeleryPublisher) PublishForgotPassword(ctx context.Context, firstName, email, uid, token, currentSite string) error {
	return publisher.publish(ctx, forgotPasswordTaskName, []any{firstName, email, uid, token, currentSite})
}

func (publisher *CeleryPublisher) PublishUserActivation(ctx context.Context, currentSite, userID string) error {
	return publisher.publish(ctx, userActivationTaskName, []any{currentSite, userID})
}

func (publisher *CeleryPublisher) PublishEmailUpdateCode(ctx context.Context, email, token string) error {
	return publisher.publish(ctx, emailUpdateCodeTaskName, []any{email, token})
}

func (publisher *CeleryPublisher) PublishEmailUpdateConfirmation(ctx context.Context, email string) error {
	return publisher.publish(ctx, emailUpdateConfirmationTaskName, []any{email})
}

func (publisher *CeleryPublisher) PublishUserDeactivation(ctx context.Context, currentSite, userID string) error {
	return publisher.publish(ctx, userDeactivationTaskName, []any{currentSite, userID})
}

func (publisher *CeleryPublisher) publish(ctx context.Context, taskName string, arguments []any) error {
	message, err := celeryMessage(taskName, arguments)
	if err != nil {
		return err
	}
	connection, err := amqp.Dial(publisher.brokerURL)
	if err != nil {
		return fmt.Errorf("connect to Celery broker: %w", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open Celery broker channel: %w", err)
	}
	defer channel.Close()
	if err := channel.PublishWithContext(ctx, "", "celery", false, false, message); err != nil {
		return fmt.Errorf("publish Celery task: %w", err)
	}
	return nil
}

func celeryMessage(taskName string, arguments []any) (amqp.Publishing, error) {
	taskID, err := randomUUID()
	if err != nil {
		return amqp.Publishing{}, err
	}
	body, err := json.Marshal([]any{
		arguments,
		map[string]any{},
		map[string]any{"callbacks": nil, "errbacks": nil, "chain": nil, "chord": nil},
	})
	if err != nil {
		return amqp.Publishing{}, fmt.Errorf("encode Celery task: %w", err)
	}
	return amqp.Publishing{
		Headers: amqp.Table{
			"lang": "py", "task": taskName, "id": taskID, "shadow": nil,
			"eta": nil, "expires": nil, "group": nil, "group_index": nil,
			"retries": int32(0), "timelimit": []any{nil, nil}, "root_id": taskID,
			"parent_id": nil, "argsrepr": fmt.Sprint(arguments), "kwargsrepr": "{}",
			"origin": "pace-api-go", "ignore_result": false,
		},
		ContentType: "application/json", ContentEncoding: "utf-8", DeliveryMode: amqp.Persistent,
		CorrelationId: taskID, Timestamp: time.Now().UTC(), Body: body,
	}, nil
}
