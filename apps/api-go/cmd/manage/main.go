// Command manage is the manage.py commands an operator runs by hand.
//
// It is one binary with a subcommand per command, named exactly as manage.py names them, so a runbook written against `python manage.py activate_user ada@example.test` reads as `manage activate_user ada@example.test` and nothing else changes.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
	"github.com/yldm-tech/pace/apps/api-go/internal/config"
	"github.com/yldm-tech/pace/apps/api-go/internal/manage"
	"github.com/yldm-tech/pace/apps/api-go/internal/storage"
	"github.com/yldm-tech/pace/apps/api-go/internal/worker"
	"golang.org/x/term"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help" {
		usage()
		return
	}
	name := os.Args[1]
	commands := manage.Registry()
	command, known := commands[name]
	if !known {
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n\n", name)
		usage()
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	settings, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	env := manage.Environment{
		Out: os.Stdout, In: os.Stdin,
		Prompt: prompt, Secret: secret,
	}

	// The connection is opened without being used, because wait_for_db has to run against a database that is not answering yet. Everything else finds out the moment it asks.
	db, err := gorm.Open(postgres.Open(settings.DatabaseURL), &gorm.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	if pool, err := db.DB(); err == nil {
		defer pool.Close()
	}
	env.DB = db

	if client, err := auth.OpenRedis(ctx, settings.Auth.RedisURL); err == nil {
		env.Redis = client
		defer client.Close()
	}

	manage.Buckets = func() (*storage.Store, error) { return objectStore(settings) }
	manage.Mail = func() (worker.EmailSettings, worker.ConfigurationReader, worker.Mailer) {
		return emailDefaults(), auth.NewGORMRepository(db, settings.Auth.SkipEnvironmentConfig, settings.Auth.SecretKey), worker.SMTPMailer{}
	}
	manage.SecretKey = settings.Auth.SecretKey
	manage.Queue = func() worker.DelayedPublisher { return auth.NewCeleryPublisher(settings.Auth.AMQPURL) }

	if err := command.Run(ctx, env, os.Args[2:]); err != nil {
		var refused *manage.ErrCommand
		if errors.As(err, &refused) {
			fmt.Fprintf(os.Stderr, "CommandError: %s\n", refused.Message)
			os.Exit(1)
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stdout, "Usage: manage <command> [arguments]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Commands:")
	commands := manage.Registry()
	for _, name := range manage.Names() {
		fmt.Fprintf(os.Stdout, "  %-34s %s\n", name, commands[name].Help)
	}
}

// prompt reads one line, which is what input() does.
func prompt(label string) (string, error) {
	fmt.Fprint(os.Stdout, label)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// secret reads one line without echoing it, which is what getpass does. Without a terminal it falls back to reading the line, the same way getpass does when stdin is not one.
func secret(label string) (string, error) {
	fmt.Fprint(os.Stdout, label)
	if !term.IsTerminal(int(syscall.Stdin)) {
		value, err := prompt("")
		return value, err
	}
	value, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func emailDefaults() worker.EmailSettings {
	return worker.EmailSettings{
		Host:     os.Getenv("EMAIL_HOST"),
		User:     os.Getenv("EMAIL_HOST_USER"),
		Password: os.Getenv("EMAIL_HOST_PASSWORD"),
		Port:     envOrDefault("EMAIL_PORT", "587"),
		UseTLS:   envOrDefault("EMAIL_USE_TLS", "1"),
		UseSSL:   envOrDefault("EMAIL_USE_SSL", "0"),
		From:     envOrDefault("EMAIL_FROM", "Team Plane <team@mailer.plane.so>"),
	}
}

func objectStore(settings config.Config) (*storage.Store, error) {
	return storage.New(storage.Settings{
		AccessKey:        settings.Auth.AWSAccessKeyID,
		SecretKey:        settings.Auth.AWSSecretAccessKey,
		Region:           settings.Auth.AWSRegion,
		Bucket:           settings.Auth.AWSBucketName,
		Endpoint:         settings.Auth.AWSEndpointURL,
		UseMinio:         settings.Auth.UseMinio,
		MinioEndpointSSL: settings.Auth.MinioEndpointSSL,
		SignedURLExpiry:  settings.Auth.SignedURLExpiration,
		PublicEndpoint:   settings.Auth.WebURL,
	})
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
