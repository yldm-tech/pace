// Package manage is the manage.py commands an operator runs by hand.
//
// They are ordinary programs rather than endpoints, and Django gave them a common shape: arguments off the command line, a few lines on stdout, and a non-zero exit when something was wrong. That shape is kept, down to the wording, so a runbook written against manage.py still reads true.
package manage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	redis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// ErrCommand is Django's CommandError: a message for the operator and a non-zero exit, rather than a traceback.
type ErrCommand struct{ Message string }

func (err *ErrCommand) Error() string { return err.Message }

func commandError(format string, arguments ...any) error {
	return &ErrCommand{Message: fmt.Sprintf(format, arguments...)}
}

// Environment is everything the commands reach for. A command that does not need a piece leaves it nil.
type Environment struct {
	DB     *gorm.DB
	Redis  redis.UniversalClient
	Out    io.Writer
	In     io.Reader
	Prompt func(label string) (string, error)
	// Secret reads something that should not be echoed, which is what getpass does.
	Secret func(label string) (string, error)
}

// Command is one manage.py command.
type Command struct {
	Name  string
	Help  string
	Usage string
	Run   func(ctx context.Context, env Environment, arguments []string) error
}

// Registry is every command this can run, by the name manage.py knows it as.
func Registry() map[string]Command {
	commands := map[string]Command{}
	for _, command := range allCommands() {
		commands[command.Name] = command
	}
	return commands
}

// Names lists the commands in the order they should be printed.
func Names() []string {
	names := make([]string, 0)
	for name := range Registry() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Run dispatches one command by name.
func Run(ctx context.Context, env Environment, name string, arguments []string) error {
	command, known := Registry()[name]
	if !known {
		return commandError("Unknown command: %q. Use \"help\" to list the available commands.", name)
	}
	return command.Run(ctx, env, arguments)
}

func write(env Environment, format string, arguments ...any) {
	if env.Out == nil {
		return
	}
	fmt.Fprintf(env.Out, format+"\n", arguments...)
}

// flagValue reads a --name value or --name=value pair off the arguments, which is what argparse accepts for an optional argument.
func flagValue(arguments []string, name string) (string, bool) {
	prefix := "--" + name
	for index, argument := range arguments {
		if argument == prefix {
			if index+1 < len(arguments) && !strings.HasPrefix(arguments[index+1], "--") {
				return arguments[index+1], true
			}
			return "", true
		}
		if strings.HasPrefix(argument, prefix+"=") {
			return strings.TrimPrefix(argument, prefix+"="), true
		}
	}
	return "", false
}

// hasFlag reports whether a bare switch was given.
func hasFlag(arguments []string, name string) bool {
	for _, argument := range arguments {
		if argument == "--"+name {
			return true
		}
	}
	return false
}

// positional reads the arguments that are not flags, in order.
func positional(arguments []string) []string {
	values := make([]string, 0, len(arguments))
	skip := false
	for index, argument := range arguments {
		if skip {
			skip = false
			continue
		}
		if strings.HasPrefix(argument, "--") {
			// A flag written apart from its value takes the next argument with it.
			if !strings.Contains(argument, "=") && index+1 < len(arguments) && !strings.HasPrefix(arguments[index+1], "--") {
				skip = true
			}
			continue
		}
		values = append(values, argument)
	}
	return values
}

// requireDB is the error a command gives when it was started without a database, which is a misconfiguration rather than a bad argument.
var requireDB = errors.New("no database connection is configured")

func database(env Environment) (*gorm.DB, error) {
	if env.DB == nil {
		return nil, requireDB
	}
	return env.DB, nil
}
